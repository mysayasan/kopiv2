; Inno Setup script for the MyMataSan Windows x64 installer.
;
; Installs the app to Program Files (read-only home), points writable state at
; %ProgramData%\MyMataSan, and registers mymatasan.exe as a Windows service. The
; binary is service-aware (infra/apphost/service_windows.go), so the plain
; sc.exe-registered service is controlled from services.msc with no wrapper.
;
; Built in CI (release.yml, windows-latest) with:
;   ISCC.exe /DAppVersion=1.74.0 /DBinDir=<dir containing mymatasan.exe> packaging\windows\mymatasan.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef BinDir
  #define BinDir "..\..\dist\windows-installer"
#endif
#define ServiceName "MyMataSan"

[Setup]
AppId={{B7B2B0E2-6E2E-4E7A-9E3C-8B2D4B5A9F10}
AppName=MyMataSan
AppVersion={#AppVersion}
AppPublisher=mysayasan
DefaultDirName={autopf}\MyMataSan
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\mymatasan.exe
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
OutputBaseFilename=mymatasan-setup-{#AppVersion}-windows-x64
WizardStyle=modern

[Dirs]
; Writable state root (database, recordings, logs, at-rest key). LocalSystem (the
; service account) can write here.
Name: "{commonappdata}\MyMataSan"

[Files]
Source: "{#BinDir}\mymatasan.exe"; DestDir: "{app}"; Flags: ignoreversion
; Default config (seeded into the data dir on first run by the app).
Source: "..\..\deploy\dist\config.json"; DestDir: "{app}"; DestName: "config.json"; Flags: ignoreversion
; Web UI.
Source: "..\..\apps\mymatasan\static\*"; DestDir: "{app}\static"; Flags: ignoreversion recursesubdirs createallsubdirs
; AI worker scripts (model weights fetched at runtime, so not bundled).
Source: "..\..\apps\mymatasan\ai\*.py"; DestDir: "{app}\ai"; Flags: ignoreversion
Source: "..\..\apps\mymatasan\ai\requirements-*.txt"; DestDir: "{app}\ai"; Flags: ignoreversion
Source: "..\..\apps\mymatasan\ai\setup.*"; DestDir: "{app}\ai"; Flags: ignoreversion
; Stock YOLO model (small); the heavy Python/torch runtime is fetched in-app.
Source: "..\..\apps\mymatasan\ai\yolo11n.pt"; DestDir: "{app}\ai"; Flags: ignoreversion
; Service / deployment notes.
Source: "..\..\deploy\README.md"; DestDir: "{app}\deploy"; Flags: ignoreversion

[UninstallRun]
; Stop and remove the service before files are deleted.
Filename: "{sys}\sc.exe"; Parameters: "stop {#ServiceName}"; Flags: runhidden; RunOnceId: "StopSvc"
Filename: "{sys}\sc.exe"; Parameters: "delete {#ServiceName}"; Flags: runhidden; RunOnceId: "DelSvc"

[Code]
procedure RunHidden(const Cmd, Params: string);
var
  rc: Integer;
begin
  Exec(Cmd, Params, '', SW_HIDE, ewWaitUntilTerminated, rc);
end;

procedure StopAndDeleteService();
begin
  RunHidden(ExpandConstant('{sys}\sc.exe'), 'stop {#ServiceName}');
  RunHidden(ExpandConstant('{sys}\sc.exe'), 'delete {#ServiceName}');
end;

procedure InstallService();
var
  q, binPath, envData: string;
begin
  q := Chr(34);
  { binPath value: "<quoted exe>" -app mymatasan — the inner quotes handle the
    space in Program Files. }
  binPath := q + '\' + q + ExpandConstant('{app}\mymatasan.exe') + '\' + q + ' -app mymatasan' + q;
  RunHidden(ExpandConstant('{sys}\sc.exe'),
    'create {#ServiceName} binPath= ' + binPath + ' start= auto obj= LocalSystem DisplayName= ' + q + 'MyMataSan NVR' + q);
  RunHidden(ExpandConstant('{sys}\sc.exe'),
    'description {#ServiceName} ' + q + 'MyMataSan NVR / on-device camera monitor' + q);
  { Per-service environment (REG_MULTI_SZ, \0-delimited): home/data split +
    supervised restart so a factory reset / self-update exit is relaunched. }
  envData := 'MYMATASAN_HOME=' + ExpandConstant('{app}') + '\0' +
             'MYMATASAN_DATA=' + ExpandConstant('{commonappdata}\MyMataSan') + '\0' +
             'KOPIV2_SUPERVISED=1';
  RunHidden(ExpandConstant('{sys}\reg.exe'),
    'add ' + q + 'HKLM\SYSTEM\CurrentControlSet\Services\{#ServiceName}' + q +
    ' /v Environment /t REG_MULTI_SZ /d ' + q + envData + q + ' /f');
  RunHidden(ExpandConstant('{sys}\sc.exe'),
    'failure {#ServiceName} reset= 86400 actions= restart/5000/restart/5000/restart/5000');
  RunHidden(ExpandConstant('{sys}\sc.exe'), 'start {#ServiceName}');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
    { On an upgrade, stop the running service so its exe isn't locked. }
    StopAndDeleteService()
  else if CurStep = ssPostInstall then
    InstallService();
end;
