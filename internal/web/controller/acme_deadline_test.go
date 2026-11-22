package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ACME issuance can outlast the server-wide 30s WriteTimeout; the endpoint
// extends its own deadline, while ordinary routes keep the global limit.
func TestACMERequestOutlivesServerWriteTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	slow := func(c *gin.Context) {
		time.Sleep(200 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
	slowIssue := func(string) (map[string]any, error) {
		time.Sleep(200 * time.Millisecond)
		return map[string]any{"domain": "example.com"}, nil
	}
	router.POST("/acme", issueCertificateHandler(slowIssue))
	router.POST("/control", slow)

	srv := httptest.NewUnstartedServer(router)
	srv.Config.WriteTimeout = 50 * time.Millisecond
	srv.Start()
	defer srv.Close()

	post := func(path string) error {
		resp, err := http.Post(srv.URL+path, "application/x-www-form-urlencoded", nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
		}
		return nil
	}

	if err := post("/control"); err == nil {
		t.Fatal("control route survived the 50ms WriteTimeout; the test cannot observe the deadline")
	}
	if err := post("/acme"); err != nil {
		t.Fatalf("ACME route was cut by the server WriteTimeout: %v", err)
	}
}
