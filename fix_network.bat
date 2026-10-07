@echo off
title WarLink Network Fix
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\fix_ghost_adapters.ps1"
