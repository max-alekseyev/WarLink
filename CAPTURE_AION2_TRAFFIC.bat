@echo off
chcp 65001 >nul
title WarLink - AION 2 Network Traffic and Ping Capture
echo ================================================================================
echo             WARLINK: ЗАХВАТ ТРАФИКА И ДИАГНОСТИКА ПИНГА AION 2                  
echo ================================================================================
echo Запрос прав Администратора для перехвата сетевых пакетов ядра Windows...
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Start-Process powershell.exe -ArgumentList '-NoProfile -ExecutionPolicy Bypass -File \"C:\Users\Max\Desktop\Wardogs RU\scripts\capture_aion2_traffic.ps1\"' -Verb RunAs"
