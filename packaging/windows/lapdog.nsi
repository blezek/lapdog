; LapDog Windows installer.
;
; Built with NSIS, which is used because makensis runs natively on macOS: the
; whole release can be produced from the development machine with no Windows box.
; See docs/superpowers/specs/2026-08-04-lapdog-packaging.md section 5.
;
;   makensis -DVERSION=0.1.0 -DSRCEXE=../../dist/lapdog.exe packaging/windows/lapdog.nsi
;
; The payload is a single self-contained executable: the web interface, the icon
; set and the database migrations are all embedded in it. There is no runtime to
; bundle and no dependency to install.

Unicode true
ManifestDPIAware true

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef SRCEXE
  !define SRCEXE "..\..\dist\lapdog.exe"
!endif
!ifndef OUTFILE
  !define OUTFILE "..\..\dist\lapdog-${VERSION}-setup.exe"
!endif

!define APPNAME    "LapDog"
!define COMPANY    "Dan Blezek"
!define DESCRIPTION "iRacing session time tracker"
!define EXENAME    "lapdog.exe"
!define REGKEY     "Software\${APPNAME}"
!define UNINSTKEY  "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"
!define RUNKEY     "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${APPNAME} ${VERSION}"
OutFile "${OUTFILE}"
BrandingText "${APPNAME} ${VERSION}"

; Per-user install: no elevation prompt, and it matches the application's own
; per-user data directory and HKCU startup key. A tray utility has no business
; asking for administrator rights.
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\${APPNAME}"
InstallDirRegKey HKCU "${REGKEY}" "InstallDir"

SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APPNAME}"
VIAddVersionKey "FileDescription" "${DESCRIPTION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "CompanyName" "${COMPANY}"
VIAddVersionKey "LegalCopyright" "${COMPANY}"

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXENAME}"
!define MUI_FINISHPAGE_RUN_TEXT "Start ${APPNAME} now"
!define MUI_FINISHPAGE_LINK "Open the LapDog interface"
!define MUI_FINISHPAGE_LINK_LOCATION "http://localhost:47047"

!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; ---------------------------------------------------------------- install

Function .onInit
  ; iRacing is 64-bit only, so a 32-bit host could never run the sim this tool
  ; exists to watch.
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "${APPNAME} requires 64-bit Windows."
    Abort
  ${EndIf}
FunctionEnd

Section "-CheckRunning"
  ; Windows will not let a running executable be overwritten, so ask the user to
  ; quit rather than failing partway through with an opaque error.
  retry:
  FindWindow $0 "" "${APPNAME}"
  ${If} $0 != 0
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION \
      "${APPNAME} is running. Quit it from the tray icon, then choose Retry." \
      IDRETRY retry
    Abort "Installation cancelled: ${APPNAME} is still running."
  ${EndIf}
SectionEnd

Section "${APPNAME}" SecCore
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${SRCEXE}"

  WriteRegStr HKCU "${REGKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${REGKEY}" "Version" "${VERSION}"

  ; Registered under HKCU so the entry appears in Settings, Apps without needing
  ; elevation to write it.
  WriteRegStr   HKCU "${UNINSTKEY}" "DisplayName"     "${APPNAME}"
  WriteRegStr   HKCU "${UNINSTKEY}" "DisplayVersion"  "${VERSION}"
  WriteRegStr   HKCU "${UNINSTKEY}" "Publisher"       "${COMPANY}"
  WriteRegStr   HKCU "${UNINSTKEY}" "DisplayIcon"     "$INSTDIR\${EXENAME}"
  WriteRegStr   HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr   HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr   HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" 30000

  WriteUninstaller "$INSTDIR\uninstall.exe"
SectionEnd

Section "Start Menu shortcut" SecStartMenu
  CreateDirectory "$SMPROGRAMS\${APPNAME}"
  CreateShortcut "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk" "$INSTDIR\${EXENAME}" "" "$INSTDIR\${EXENAME}" 0
  CreateShortcut "$SMPROGRAMS\${APPNAME}\Uninstall ${APPNAME}.lnk" "$INSTDIR\uninstall.exe"
SectionEnd

; A tray application starts at login, so a desktop icon is clutter for most
; users. Offered, but off by default.
Section /o "Desktop shortcut" SecDesktop
  CreateShortcut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\${EXENAME}" "" "$INSTDIR\${EXENAME}" 0
SectionEnd

Section "Start with Windows" SecStartup
  ; Writes exactly the value the application manages itself, so the installer and
  ; the settings screen agree rather than competing.
  WriteRegStr HKCU "${RUNKEY}" "${APPNAME}" '"$INSTDIR\${EXENAME}"'
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecCore} \
    "The ${APPNAME} application. The web interface and icons are built into the executable, so there is nothing else to install."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecStartMenu} "Add ${APPNAME} to the Start Menu."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} "Add a shortcut to the Desktop."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecStartup} \
    "Launch ${APPNAME} automatically when you sign in, so sessions are recorded without you starting it."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

; -------------------------------------------------------------- uninstall

Section "un.${APPNAME}" UnSecCore
  SectionIn RO

  Delete "$INSTDIR\${EXENAME}"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APPNAME}\${APPNAME}.lnk"
  Delete "$SMPROGRAMS\${APPNAME}\Uninstall ${APPNAME}.lnk"
  RMDir "$SMPROGRAMS\${APPNAME}"
  Delete "$DESKTOP\${APPNAME}.lnk"

  DeleteRegValue HKCU "${RUNKEY}" "${APPNAME}"
  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "${REGKEY}"
SectionEnd

; Years of racing history must not be destroyed by an uninstall. Removing it is
; the user's decision, taken deliberately, so this is off by default.
Section /o "un.Delete my racing history" UnSecData
  RMDir /r "$LOCALAPPDATA\lapdog"
SectionEnd

!insertmacro MUI_UNFUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${UnSecCore} "Remove the ${APPNAME} program and its shortcuts."
  !insertmacro MUI_DESCRIPTION_TEXT ${UnSecData} \
    "Also delete the database, settings, log and capture files in %LOCALAPPDATA%\lapdog. This cannot be undone."
!insertmacro MUI_UNFUNCTION_DESCRIPTION_END
