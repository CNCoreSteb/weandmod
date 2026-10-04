@echo off
rem Build WeAndMod.exe (single file, no console window).
rem Requires: Go in PATH, MinGW gcc in PATH (Fyne needs CGO).
setlocal
set CGO_ENABLED=1
go build -trimpath -ldflags "-H windowsgui -s -w" -o WeAndMod.exe .
endlocal
