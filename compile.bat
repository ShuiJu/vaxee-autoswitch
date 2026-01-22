@echo off
:: Delete old exe
if exist "vaxee-autoswitch.exe" (
    echo Deleting older version...
    del /f "vaxee-autoswitch.exe"
)

:: compile
echo Compiling...
go build -trimpath -ldflags "-s -w" -o vaxee-autoswitch.exe

:: Check if success
if errorlevel 1 (
    echo success
    pause
    exit /b 1
)

echo Finished
if exist "vaxee-autoswitch.exe" pause
