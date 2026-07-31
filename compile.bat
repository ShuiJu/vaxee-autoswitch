@echo off
setlocal

:: Read appVersion from version.go (line 5 in current file), filename follows version
:: Layout assumption: line5 is `appVersion     = "Vx.y.z"`
for /f "skip=4 tokens=3" %%a in (version.go) do (
  set "APP_VERSION=%%a"
  goto :end_ver
)
:end_ver
if not defined APP_VERSION (
  echo Failed to read appVersion from version.go
  exit /b 1
)
:: Strip surrounding quotes -> Vx.y.z
set "APP_VERSION=%APP_VERSION:~1,-1%"

set "OUTPUT_EXE=vaxee-autoswitch.%APP_VERSION%.exe"
set "RSRC_EXE=%USERPROFILE%\go\bin\rsrc.exe"
set "SYSO_FILE=rsrc_windows_amd64.syso"

:: Delete old exe
if exist "%OUTPUT_EXE%" (
    echo Deleting older version...
    del /f "%OUTPUT_EXE%"
)

if exist "rsrc.syso" del /f "rsrc.syso"
if exist "%SYSO_FILE%" del /f "%SYSO_FILE%"

if exist "%RSRC_EXE%" goto have_rsrc
echo rsrc.exe not found: %RSRC_EXE%
exit /b 1

:have_rsrc
echo Building Windows icon resources...
"%RSRC_EXE%" -arch amd64 -ico "vaxee-icon.ico" -o "%SYSO_FILE%"
if errorlevel 1 (
    echo Failed to build icon resource.
    exit /b 1
)

:: compile
echo Compiling...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o "%OUTPUT_EXE%"

:: Check if success
if errorlevel 1 (
    echo Build failed.
    pause
    exit /b 1
)

echo Finished
