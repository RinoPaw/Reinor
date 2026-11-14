@echo off
setlocal
cd /d "%~dp0"
set "DEVICE=temp-support-%RANDOM%%RANDOM%"
echo Reinor temporary support session
echo.
echo This grants the approved operator the ability to run commands with your Windows user permissions.
echo Keep this window open only while you consent to assistance.
echo You may close this window to disconnect.
echo.
"%~dp0reinor-agent.exe" join -temporary -device "%DEVICE%"
echo.
echo Reinor session ended.
pause
