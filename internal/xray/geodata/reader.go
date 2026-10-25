package geodata

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrUnrecognized reports a file that parses as neither database layout, which
// in practice means a truncated download or an unrelated file renamed to .dat.
var ErrUnrecognized = errors.New("file is not a geosite or geoip database")

// errInvalidProtoField reports a wire form these databases never use, such as
// a protobuf group or a field number that does not fit.
var errInvalidProtoField = errors.New("invalid protobuf field")

const (
	fieldListEntry     = 1
	fieldEntryCode     = 1
	fieldEntryPayload  = 2
	fieldDomainType    = 1
	fieldDomainValue   = 2
	fieldDomainAttr    = 3
	fieldAttrKey       = 1
	fieldCIDRAddress   = 1
	fieldCIDRPrefixLen = 2
)

const (
	domainTypeSubstr = 0
	domainTypeRegex  = 1
	domainTypeFull   = 3
)

type categoryScan struct {
	kind       GeoKind
	categories []GeoCategory
	spans      map[string][]byteSpan
	usable     int
}

// byteSpan locates one category's record inside the database file, so a page of
// its rules can be read without pulling the whole file into memory again.
type byteSpan struct {
	offset int64
	length int64
}

// scanIndex walks the database once and reports every category with its entry
// count and attribute keys, holding only one top-level record at a time.
func scanIndex(reader *geoReader, kind GeoKind) (*categoryScan, error) {
	scan := &categoryScan{kind: kind, spans: make(map[string][]byteSpan)}
	byCode := make(map[string]int)

	err := eachListEntry(reader, func(entry []byte, span byteSpan) error {
		count := 0
		attributes := make(map[string]struct{})
		code, err := walkEntry(entry, func(payload []byte) error {
			if kind == KindSite {
				value, attrs, err := domainValue(payload)
				if err != nil {
					return err
				}
				if len(value) == 0 {
					return nil
				}
				for _, attr := range attrs {
					attributes[attr] = struct{}{}
				}
			} else {
				_, ok, err := cidrBytes(payload)
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
			}
			count++
			return nil
		})
		if err != nil || code == "" {
			return err
		}
		scan.usable += count
		scan.spans[code] = append(scan.spans[code], span)
		if position, seen := byCode[code]; seen {
			scan.categories[position].Entries += count
			scan.categories[position].Attributes = mergeAttributes(scan.categories[position].Attributes, attributes)
			return nil
		}
		byCode[code] = len(scan.categories)
		scan.categories = append(scan.categories, GeoCategory{
			Code:       code,
			Entries:    count,
			Attributes: mergeAttributes(nil, attributes),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(scan.categories, func(i, j int) bool { return scan.categories[i].Code < scan.categories[j].Code })
	return scan, nil
}

// scanEntries walks the database once and materialises only the requested page
// of one category, so browsing a category with hundreds of thousands of rules
// costs no more than browsing a small one.
func scanEntries(records [][]byte, kind GeoKind, code, query string, offset, limit int) (GeoEntryPage, error) {
	page := GeoEntryPage{Items: []GeoEntry{}}
	matched := 0

	for _, entry := range records {
		// Values stay as raw bytes until a row is known to belong on the
		// requested page: turning all 170k rules of a category into strings
		// to serve one screenful is what made this expensive.
		if _, err := walkEntry(entry, func(payload []byte) error {
			var raw []byte
			var ok bool
			var err error
			if kind == KindSite {
				raw, _, err = domainValue(payload)
				if err != nil {
					return err
				}
				ok = len(raw) > 0
			} else {
				raw, ok, err = cidrBytes(payload)
				if err != nil {
					return err
				}
			}
			if !ok {
				return nil
			}
			if query != "" && !containsFold(raw, query) {
				return nil
			}
			if matched >= offset && len(page.Items) < limit {
				if kind == KindSite {
					page.Items = append(page.Items, GeoEntry{Kind: domainKind(payload), Value: string(raw)})
				} else {
					page.Items = append(page.Items, GeoEntry{Kind: "cidr", Value: string(raw)})
				}
			}
			matched++
			return nil
		}); err != nil {
			return GeoEntryPage{}, err
		}
	}
	page.Total = matched
	return page, nil
}

// detectKind reports which layout the file uses. The two share a wire layout
// whose field types disagree, so decoding one as the other yields no usable
// values at all — the count of readable entries is what tells them apart. The
// file name only picks which layout to try first, so the common case scans once.
func detectKind(dir, name string) (GeoKind, *categoryScan, error) {
	first, second := KindSite, KindIP
	if strings.Contains(strings.ToLower(name), "ip") {
		first, second = KindIP, KindSite
	}
	var firstErr error
	for _, kind := range [...]GeoKind{first, second} {
		scan, err := scanDatabase(dir, name, kind)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if scan.usable > 0 {
			return kind, scan, nil
		}
	}
	if firstErr != nil {
		// A truncated download is the common case here, and it reads very
		// differently to the user than "this is not a geo database at all".
		return "", nil, fmt.Errorf("%w: %w", ErrUnrecognized, firstErr)
	}
	return "", nil, ErrUnrecognized
}

// readSpans reads only the recorded slices of the file, so serving a page of a
// category costs its own record rather than the whole database. The handle is
// opened through an os.Root for the same reason the index scan is.
func readSpans(dir, name string, spans []byteSpan) ([][]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records := make([][]byte, 0, len(spans))
	for _, span := range spans {
		if span.length <= 0 || span.length > MaxFileSize {
			return nil, ErrUnrecognized
		}
		record := make([]byte, span.length)
		if _, err := file.ReadAt(record, span.offset); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// geoReader reads a database through a small buffer while tracking the absolute
// offset. Reading one top-level record at a time is what keeps a 70 MB
// geosite_RU from materialising in the panel's heap: only the record being
// inspected is allocated, not the whole file.
type geoReader struct {
	br   *bufio.Reader
	size int64
	pos  int64
}

const geoReadBufferSize = 64 << 10

func newGeoReader(file *os.File, size int64) *geoReader {
	return &geoReader{br: bufio.NewReaderSize(file, geoReadBufferSize), size: size}
}

// ReadByte satisfies io.ByteReader, so binary.ReadUvarint can read from the
// stream without a slice of the file.
func (r *geoReader) ReadByte() (byte, error) {
	b, err := r.br.ReadByte()
	if err == nil {
		r.pos++
	}
	return b, err
}

func (r *geoReader) readTag() (protowire.Number, protowire.Type, error) {
	value, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, 0, err
	}
	number, wireType := protowire.DecodeTag(value)
	if number < protowire.MinValidNumber {
		return 0, 0, errInvalidProtoField
	}
	return number, wireType, nil
}

// readLength reads a length-delimited field's size and rejects one that would
// run past the end of the file before allocating anything for it.
func (r *geoReader) readLength() (int64, error) {
	value, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, err
	}
	if value > uint64(r.size-r.pos) {
		return 0, io.ErrUnexpectedEOF
	}
	return int64(value), nil
}

func (r *geoReader) discard(n int64) error {
	if n < 0 || n > r.size-r.pos {
		return io.ErrUnexpectedEOF
	}
	discarded, err := r.br.Discard(int(n))
	r.pos += int64(discarded)
	if err != nil {
		return err
	}
	if int64(discarded) != n {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (r *geoReader) skipField(number protowire.Number, wireType protowire.Type) error {
	if number <= 0 {
		return errInvalidProtoField
	}
	switch wireType {
	case protowire.VarintType:
		_, err := binary.ReadUvarint(r)
		return err
	case protowire.Fixed32Type:
		return r.discard(4)
	case protowire.Fixed64Type:
		return r.discard(8)
	case protowire.BytesType:
		length, err := r.readLength()
		if err != nil {
			return err
		}
		return r.discard(length)
	default:
		// Geo databases never use protobuf groups or the reserved wire types.
		return errInvalidProtoField
	}
}

// nextListEntry advances to the next top-level list record and returns its
// payload plus its location. buf is reused across calls so a database with
// thousands of categories allocates only its largest record.
func (r *geoReader) nextListEntry(buf []byte) ([]byte, byteSpan, bool, error) {
	for r.pos < r.size {
		number, wireType, err := r.readTag()
		if err != nil {
			return nil, byteSpan{}, false, err
		}
		if number != fieldListEntry || wireType != protowire.BytesType {
			if err := r.skipField(number, wireType); err != nil {
				return nil, byteSpan{}, false, err
			}
			continue
		}

		length, err := r.readLength()
		if err != nil {
			return nil, byteSpan{}, false, err
		}
		if int64(cap(buf)) < length {
			buf = make([]byte, length)
		}
		buf = buf[:length]
		if _, err := io.ReadFull(r.br, buf); err != nil {
			return nil, byteSpan{}, false, err
		}
		span := byteSpan{offset: r.pos, length: length}
		r.pos += length
		return buf, span, true, nil
	}
	return nil, byteSpan{}, false, nil
}

// eachListEntry walks every top-level record in the database. The callback must
// not retain entry: its backing array is reused for the next record.
func eachListEntry(reader *geoReader, visit func(entry []byte, span byteSpan) error) error {
	var buf []byte
	for {
		entry, span, ok, err := reader.nextListEntry(buf)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		buf = entry[:0]
		if err := visit(entry, span); err != nil {
			return err
		}
	}
}

// scanDatabase opens one database through an os.Root rooted at the asset
// directory and scans it as kind. Going through the root rather than a joined
// path means the file name — which arrives from an HTTP request — never becomes
// a path this code resolves itself: a symlink planted in the folder, or swapped
// in between the check and the read, cannot pull in a file from elsewhere on
// disk.
func scanDatabase(dir, name string, kind GeoKind) (*categoryScan, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidName
	}
	if info.Size() > MaxFileSize {
		return nil, ErrFileTooLarge
	}
	return scanIndex(newGeoReader(file, info.Size()), kind)
}

// walkEntry reports a record's category code and hands each rule to visit.
// The rules are not collected into a slice first: a single category can hold
// a hundred thousand of them, and that slice was the bulk of what serving one
// page allocated.
func walkEntry(entry []byte, visit func(payload []byte) error) (string, error) {
	code := ""
	for len(entry) > 0 {
		number, wireType, consumed := protowire.ConsumeTag(entry)
		if consumed < 0 {
			return "", protowire.ParseError(consumed)
		}
		entry = entry[consumed:]
		switch {
		case number == fieldEntryCode && wireType == protowire.BytesType:
			value, size := protowire.ConsumeBytes(entry)
			if size < 0 {
				return "", protowire.ParseError(size)
			}
			code = strings.ToLower(string(value))
			entry = entry[size:]
		case number == fieldEntryPayload && wireType == protowire.BytesType:
			payload, size := protowire.ConsumeBytes(entry)
			if size < 0 {
				return "", protowire.ParseError(size)
			}
			if visit != nil {
				if err := visit(payload); err != nil {
					return "", err
				}
			}
			entry = entry[size:]
		default:
			size := protowire.ConsumeFieldValue(number, wireType, entry)
			if size < 0 {
				return "", protowire.ParseError(size)
			}
			entry = entry[size:]
		}
	}
	return code, nil
}

func containsFold(haystack []byte, needle string) bool {
	return strings.Contains(strings.ToLower(string(haystack)), needle)
}

func domainValue(payload []byte) ([]byte, []string, error) {
	var value []byte
	var attributes []string
	for len(payload) > 0 {
		number, wireType, consumed := protowire.ConsumeTag(payload)
		if consumed < 0 {
			return nil, nil, protowire.ParseError(consumed)
		}
		payload = payload[consumed:]
		switch {
		case number == fieldDomainValue && wireType == protowire.BytesType:
			raw, size := protowire.ConsumeBytes(payload)
			if size < 0 {
				return nil, nil, protowire.ParseError(size)
			}
			value = raw
			payload = payload[size:]
		case number == fieldDomainAttr && wireType == protowire.BytesType:
			raw, size := protowire.ConsumeBytes(payload)
			if size < 0 {
				return nil, nil, protowire.ParseError(size)
			}
			if key := attributeKey(raw); key != "" {
				attributes = append(attributes, key)
			}
			payload = payload[size:]
		default:
			size := protowire.ConsumeFieldValue(number, wireType, payload)
			if size < 0 {
				return nil, nil, protowire.ParseError(size)
			}
			payload = payload[size:]
		}
	}
	return value, attributes, nil
}

func attributeKey(attribute []byte) string {
	for len(attribute) > 0 {
		number, wireType, consumed := protowire.ConsumeTag(attribute)
		if consumed < 0 {
			return ""
		}
		attribute = attribute[consumed:]
		if number == fieldAttrKey && wireType == protowire.BytesType {
			raw, size := protowire.ConsumeBytes(attribute)
			if size < 0 {
				return ""
			}
			return strings.ToLower(string(raw))
		}
		size := protowire.ConsumeFieldValue(number, wireType, attribute)
		if size < 0 {
			return ""
		}
		attribute = attribute[size:]
	}
	return ""
}

// domainKind maps a domain's match type. proto3 omits zero values, so a domain
// with no type field on the wire is a Substr (keyword) rule, not a domain one.
func domainKind(payload []byte) string {
	matchType := uint64(domainTypeSubstr)
	for len(payload) > 0 {
		number, wireType, consumed := protowire.ConsumeTag(payload)
		if consumed < 0 {
			break
		}
		payload = payload[consumed:]
		if number == fieldDomainType && wireType == protowire.VarintType {
			raw, size := protowire.ConsumeVarint(payload)
			if size < 0 {
				break
			}
			matchType = raw
			break
		}
		size := protowire.ConsumeFieldValue(number, wireType, payload)
		if size < 0 {
			break
		}
		payload = payload[size:]
	}
	switch matchType {
	case domainTypeFull:
		return "full"
	case domainTypeRegex:
		return "regexp"
	case domainTypeSubstr:
		return "keyword"
	default:
		return "domain"
	}
}

// cidrBytes renders one CIDR. proto3 omits zero values, so a missing prefix
// field means /0 — a default route, which a hand-built ext: database may well
// contain — and must not be read as "no prefix given".
func cidrBytes(payload []byte) ([]byte, bool, error) {
	var address []byte
	prefix := uint64(0)
	for len(payload) > 0 {
		number, wireType, consumed := protowire.ConsumeTag(payload)
		if consumed < 0 {
			return nil, false, protowire.ParseError(consumed)
		}
		payload = payload[consumed:]
		switch {
		case number == fieldCIDRAddress && wireType == protowire.BytesType:
			raw, size := protowire.ConsumeBytes(payload)
			if size < 0 {
				return nil, false, protowire.ParseError(size)
			}
			address = raw
			payload = payload[size:]
		case number == fieldCIDRPrefixLen && wireType == protowire.VarintType:
			raw, size := protowire.ConsumeVarint(payload)
			if size < 0 {
				return nil, false, protowire.ParseError(size)
			}
			prefix = raw
			payload = payload[size:]
		default:
			size := protowire.ConsumeFieldValue(number, wireType, payload)
			if size < 0 {
				return nil, false, protowire.ParseError(size)
			}
			payload = payload[size:]
		}
	}
	addr, ok := netip.AddrFromSlice(address)
	if !ok || prefix > uint64(addr.BitLen()) {
		return nil, false, nil
	}
	return []byte(netip.PrefixFrom(addr, int(prefix)).String()), true, nil
}

// mergeAttributes always returns a non-nil slice: the JSON contract declares
// attributes as an array, and a nil slice would marshal to null and break
// clients validating against it.
func mergeAttributes(existing []string, attributes map[string]struct{}) []string {
	if len(attributes) == 0 {
		if existing == nil {
			return []string{}
		}
		return existing
	}
	merged := make(map[string]struct{}, len(existing)+len(attributes))
	for _, attribute := range existing {
		merged[attribute] = struct{}{}
	}
	for attribute := range attributes {
		merged[attribute] = struct{}{}
	}
	out := make([]string, 0, len(merged))
	for attribute := range merged {
		out = append(out, attribute)
	}
	sort.Strings(out)
	return out
}
