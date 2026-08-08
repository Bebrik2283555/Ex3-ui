@echo off
setlocal enabledelayedexpansion

rem =====================================================================
rem  build-xui.bat — one-shot build for the 3x-ui fork
rem  Windows host (no gcc/WSL/Docker): cross-compiles the Linux x64
rem  binary with CGO disabled (pure-Go SQLite driver), so the result
rem  runs on the Debian 12 VPS.
rem
rem  Steps:
rem    1. build the Vite bundles into internal/web/dist
rem    2. (optional) run Go unit tests
rem    3. cross-compile main.go for GOOS=linux CGO_ENABLED=0
rem    4. copy the binary into dist\
rem =====================================================================

set "ROOT=%~dp0"
cd /d "%ROOT%"

set "OUT_DIR=%ROOT%dist"
set "OUT_BIN=%OUT_DIR%\x-ui"
set "GOFLAGS=-trimpath"
set "LDFLAGS=-s -w"

if not exist "%OUT_DIR%" mkdir "%OUT_DIR%"

echo[====] Building frontend (npm run build)...
pushd "%ROOT%frontend"
call npm run build
if errorlevel 1 (
    echo[FAIL] Frontend build failed.
    exit /b 1
)
popd

echo[====] Go vet...
go vet ./...
if errorlevel 1 (
    echo[FAIL] go vet failed.
    exit /b 1
)

echo[====] Go tests...
go test ./internal/extra/... ./internal/web/... ./internal/optimize/... ./internal/zapret/... ./internal/hostsfile/...
if errorlevel 1 (
    echo[FAIL] Go tests failed.
    exit /b 1
)

echo[====] Cross-compiling Linux amd64 binary...
set "GOOS=linux"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
go build -trimpath -ldflags "%LDFLAGS%" -o "%OUT_BIN%" .
set "BUILD_ERR=%errorlevel%"

set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"

if not "%BUILD_ERR%"=="0" (
    echo[FAIL] Cross-compile failed.
    exit /b 1
)

echo[OK] Binary written to: %OUT_BIN%
for /f "delims=" %%v in (internal\config\version) do set "PANEL_VERSION=%%v"
echo     Version: %PANEL_VERSION%
echo     Copy to the VPS, then:
echo         chmod +x x-ui
echo         ./x-ui
exit /b 0
