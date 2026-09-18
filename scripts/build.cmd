@echo off
setlocal EnableExtensions

set "interactive=0"
if "%~1"=="" set "interactive=1"

if "%interactive%"=="1" (
    echo Astra MWE Windows builder
    echo.
    echo   1. Windows x64 ^(amd64^)
    echo   2. Windows ARM64
    echo.
    choice /c 12 /n /m "Choose target [1/2]: "
    if errorlevel 2 (
        set "architecture=arm64"
    ) else (
        set "architecture=amd64"
    )
) else (
    set "architecture=%~1"
)

if /i "%architecture%"=="amd64" goto architecture_ok
if /i "%architecture%"=="arm64" goto architecture_ok
echo ERROR: Expected architecture amd64 or arm64. 1>&2
goto failed

:architecture_ok
where go >nul 2>nul || (
    echo ERROR: Go 1.25 or newer was not found in PATH. 1>&2
    goto failed
)
where node >nul 2>nul || (
    echo ERROR: Node.js 24 was not found in PATH. 1>&2
    goto failed
)
where npm >nul 2>nul || (
    echo ERROR: npm was not found in PATH. 1>&2
    goto failed
)

cd /d "%~dp0.." || (
    echo ERROR: Could not open the project directory. 1>&2
    goto failed
)

echo.
echo [1/5] Generating application icons...
set "GOARCH="
call go run ./cmd/package-icon || goto command_failed

echo [2/5] Installing frontend dependencies...
pushd frontend || goto command_failed
call npm ci || (popd & goto command_failed)

echo [3/5] Building frontend...
call npm run build || (popd & goto command_failed)
popd

if not exist "build\bin" mkdir "build\bin" || goto command_failed
set "GOARCH=%architecture%"

echo [4/5] Building Windows GUI for %architecture%...
call go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui" -o "build/bin/astra-mwe-windows-%architecture%.exe" . || goto command_failed

echo [5/5] Building Windows CLI for %architecture%...
call go build -trimpath -ldflags "-s -w" -o "build/bin/astra-cli-windows-%architecture%.exe" ./cmd/astra-cli || goto command_failed

echo.
echo Build completed successfully.
echo GUI: build\bin\astra-mwe-windows-%architecture%.exe
echo CLI: build\bin\astra-cli-windows-%architecture%.exe
if "%interactive%"=="1" pause
exit /b 0

:command_failed
echo. 1>&2
echo ERROR: Build failed. See the command output above. 1>&2

:failed
if "%interactive%"=="1" pause
exit /b 1
