@echo off
REM Diagnostico do Lumnia Zap no Windows.
REM Roda como o proprio usuario, nao precisa de Administrador.
REM Nao instala nem apaga nada: so olha e anota.

setlocal
set "RAIZ=%LOCALAPPDATA%\LumniaZap"
set "PONTE=%RAIZ%\whatsapp-bridge\whatsapp-bridge.exe"

REM Escreve primeiro no TEMP, que sempre existe. So depois tenta levar para a
REM Area de Trabalho. Em Windows em portugues com OneDrive a pasta se chama
REM "Area de Trabalho" e nao "Desktop", entao perguntamos ao proprio Windows
REM qual e o caminho em vez de chutar.
set "TMPOUT=%TEMP%\diagnostico-lumnia-zap.txt"

echo.
echo  Diagnostico Lumnia Zap
echo  ----------------------
echo  Isto leva cerca de 40 segundos. Nao feche a janela.
echo.

call :coletar > "%TMPOUT%" 2>&1

set "MESA="
for /f "usebackq delims=" %%D in (`powershell -NoProfile -ExecutionPolicy Bypass -Command "[Environment]::GetFolderPath('Desktop')" 2^>nul`) do set "MESA=%%D"

set "SAIDA=%TMPOUT%"
if defined MESA if exist "%MESA%\" (
  copy /y "%TMPOUT%" "%MESA%\diagnostico-lumnia-zap.txt" >nul 2>&1
  if not errorlevel 1 set "SAIDA=%MESA%\diagnostico-lumnia-zap.txt"
)

echo.
if exist "%SAIDA%" (
  echo  Pronto. O arquivo foi salvo em:
  echo    %SAIDA%
  echo.
  echo  Mande esse arquivo para o Diego.
  echo  Vou abrir a pasta onde ele esta.
  explorer /select,"%SAIDA%"
) else (
  echo  ATENCAO: nao consegui salvar o arquivo em lugar nenhum.
  echo  Tire uma foto desta janela e mande para o Diego.
)
echo.
pause
exit /b

REM ------------------------------------------------------------------

:coletar
echo ===== DIAGNOSTICO LUMNIA ZAP =====
date /t
time /t
ver
echo Usuario: %USERNAME%
echo Arquitetura: %PROCESSOR_ARCHITECTURE%
echo.

echo ----- 1. A pasta de instalacao existe? -----
if exist "%RAIZ%" (
  echo SIM: %RAIZ%
  dir /s /b "%RAIZ%"
) else (
  echo NAO. A pasta %RAIZ% nao existe.
)
echo.

echo ----- 2. O executavel da ponte existe? -----
if exist "%PONTE%" (
  echo SIM
  dir "%PONTE%"
) else (
  echo NAO. Arquivo ausente: %PONTE%
  echo Suspeita principal: antivirus removeu o arquivo depois de extraido.
)
echo.

echo ----- 3. Alguem ja esta usando a porta 8080? -----
netstat -ano | findstr ":8080"
if errorlevel 1 echo Porta 8080 livre.
echo.

echo ----- 4. O VBScript funciona nesta maquina? -----
echo WScript.Echo "vbscript funcionando" > "%TEMP%\lz-teste.vbs"
cscript //nologo "%TEMP%\lz-teste.vbs"
if errorlevel 1 echo FALHOU: o Windows nao esta executando VBScript.
del "%TEMP%\lz-teste.vbs" >nul 2>&1
echo.

echo ----- 5. Os scripts do servico foram criados? -----
if exist "%RAIZ%\watchdog.vbs" (echo watchdog.vbs: SIM) else (echo watchdog.vbs: NAO)
if exist "%RAIZ%\abrir.vbs" (echo abrir.vbs: SIM) else (echo abrir.vbs: NAO)
if exist "%RAIZ%\iniciar.cmd" (echo iniciar.cmd: SIM) else (echo iniciar.cmd: NAO - versao antiga do instalador)
echo.

echo ----- 6. O atalho de inicializacao foi criado? -----
dir "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup"
echo.

echo ----- 7. Onde esta a configuracao do Claude? -----
echo [caminho classico]
if exist "%APPDATA%\Claude\claude_desktop_config.json" (
  type "%APPDATA%\Claude\claude_desktop_config.json"
) else (
  echo nao existe: %APPDATA%\Claude\claude_desktop_config.json
)
echo.
echo [caminho da Microsoft Store - MSIX]
dir /b "%LOCALAPPDATA%\Packages\Claude_*" 2>nul
if errorlevel 1 echo nenhum pacote Claude_* encontrado
for /d %%P in ("%LOCALAPPDATA%\Packages\Claude_*") do (
  echo --- %%P
  if exist "%%P\LocalCache\Roaming\Claude\claude_desktop_config.json" (
    type "%%P\LocalCache\Roaming\Claude\claude_desktop_config.json"
  ) else (
    echo sem claude_desktop_config.json aqui
  )
)
echo.

echo ----- 8. O uv foi instalado? -----
if exist "%USERPROFILE%\.local\bin\uv.exe" (
  echo SIM: %USERPROFILE%\.local\bin\uv.exe
) else (
  echo NAO encontrado em %USERPROFILE%\.local\bin\uv.exe
  where uv 2>&1
)
echo.

echo ----- 9. RODANDO A PONTE (25 segundos) -----
echo Esta e a parte que importa: aqui aparece o erro de verdade.
echo.
if not exist "%PONTE%" (
  echo Pulado: o executavel nao existe.
  goto :fim
)
taskkill /f /im whatsapp-bridge.exe >nul 2>&1
cd /d "%RAIZ%\whatsapp-bridge"
start "" /b cmd /c ""%PONTE%" > "%TEMP%\lz-ponte.txt" 2>&1"
timeout /t 25 /nobreak >nul
echo --- saida da ponte: ---
if exist "%TEMP%\lz-ponte.txt" (type "%TEMP%\lz-ponte.txt") else (echo NENHUMA SAIDA - o processo morreu antes de escrever qualquer coisa.)
echo --- fim da saida ---
echo.
echo Ela chegou a responder na porta 8080?
netstat -ano | findstr ":8080"
if errorlevel 1 echo NAO respondeu.
taskkill /f /im whatsapp-bridge.exe >nul 2>&1
del "%TEMP%\lz-ponte.txt" >nul 2>&1
echo.

echo ----- 10. O log da ponte tem algo? -----
if exist "%RAIZ%\logs\bridge.log" (
  type "%RAIZ%\logs\bridge.log"
) else (
  echo log inexistente
)
echo.

echo ----- 11. O Defender bloqueou alguma coisa? -----
powershell -NoProfile -ExecutionPolicy Bypass -Command "try { Get-MpThreatDetection ^| Select-Object -Last 5 ^| Format-List InitialDetectionTime,ThreatID,Resources } catch { 'nao foi possivel consultar o Defender' }"
echo.

:fim
echo ===== FIM DO DIAGNOSTICO =====
exit /b
