; Inno Setup script for the MySeliaSan Windows x64 installer.
;
; Installs the app to Program Files (read-only home), points writable state at
; %ProgramData%\MySeliaSan, and registers myseliasan.exe as a Windows service. The
; binary is service-aware (infra/apphost/service_windows.go), so the plain
; sc.exe-registered service is controlled from services.msc with no wrapper.
;
; Mirrors mymatasan.iss (same structure, same generated-password finish page). The
; differences are the port (3002), the data root, and the payload — myseliasan is pure
; Go, so there is no ai/ dir and no model weights to bundle.
;
; Built in CI (release-myseliasan.yml, windows-latest) with:
;   ISCC.exe /DAppVersion=1.9.0 /DBinDir=<dir containing myseliasan.exe> packaging\windows\myseliasan.iss

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef BinDir
  #define BinDir "..\..\dist\windows-installer-myseliasan"
#endif
#define ServiceName "MySeliaSan"
#define AppUrl "https://localhost:3002"

[Setup]
; Distinct from mymatasan's AppId — they are separate products and may be installed
; side by side on the same host.
AppId={{3F7C1A64-9D28-4E51-B0A7-6C5E2D9F4B13}
AppName=MySeliaSan
AppVersion={#AppVersion}
AppPublisher=mysayasan
AppPublisherURL=https://github.com/mysayasan/kopiv2
DefaultDirName={autopf}\MySeliaSan
DefaultGroupName=MySeliaSan
UninstallDisplayIcon={app}\myseliasan.exe
SetupIconFile=myseliasan.ico
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
OutputBaseFilename=myseliasan-setup-{#AppVersion}-windows-x64
WizardStyle=modern

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
; Only shown on a reinstall over existing data (Check: IsUpgrade). Ticking it resets the
; admin login to a fresh generated password shown on the finish page — the recovery path
; when the operator is locked out of an existing install.
Name: "resetadmin"; Description: "Reset the admin login (use if you are locked out of this existing install)"; GroupDescription: "Existing installation:"; Flags: unchecked; Check: IsUpgrade

[Dirs]
; Writable state root (database, logs, certs, at-rest key). LocalSystem (the service
; account) can write here.
Name: "{commonappdata}\MySeliaSan"

[Files]
Source: "{#BinDir}\myseliasan.exe"; DestDir: "{app}"; Flags: ignoreversion
; Brand icon, installed so the Start Menu / desktop shortcuts can reference it.
Source: "myseliasan.ico"; DestDir: "{app}"; Flags: ignoreversion
; Default config (seeded into the data dir on first run by the app).
Source: "..\..\deploy\dist\myseliasan-config.json"; DestDir: "{app}"; DestName: "config.json"; Flags: ignoreversion
; Web UI.
Source: "..\..\apps\myseliasan\static\*"; DestDir: "{app}\static"; Flags: ignoreversion recursesubdirs createallsubdirs
; Service / deployment notes.
Source: "..\..\deploy\README-myseliasan.md"; DestDir: "{app}\deploy"; DestName: "README.md"; Flags: ignoreversion

[Icons]
; Main launcher: opens the web UI in the default browser.
Name: "{group}\MySeliaSan"; Filename: "{#AppUrl}"; IconFilename: "{app}\myseliasan.ico"; Comment: "Open the MySeliaSan web console"
Name: "{commondesktop}\MySeliaSan"; Filename: "{#AppUrl}"; IconFilename: "{app}\myseliasan.ico"; Tasks: desktopicon
; Service control (self-elevating via PowerShell so a non-admin can still toggle it).
Name: "{group}\Start MySeliaSan"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -WindowStyle Hidden -Command ""Start-Process sc.exe -Verb RunAs -ArgumentList 'start','{#ServiceName}'"""; IconFilename: "{app}\myseliasan.ico"; Comment: "Start the MySeliaSan service"
Name: "{group}\Stop MySeliaSan"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -WindowStyle Hidden -Command ""Start-Process sc.exe -Verb RunAs -ArgumentList 'stop','{#ServiceName}'"""; IconFilename: "{app}\myseliasan.ico"; Comment: "Stop the MySeliaSan service"
Name: "{group}\Windows Services (manage MySeliaSan)"; Filename: "{sys}\services.msc"; Comment: "Manage the MySeliaSan service in the Services console"
Name: "{group}\{cm:UninstallProgram,MySeliaSan}"; Filename: "{uninstallexe}"

[UninstallRun]
; Stop and remove the service before files are deleted.
Filename: "{sys}\sc.exe"; Parameters: "stop {#ServiceName}"; Flags: runhidden; RunOnceId: "StopSvc"
Filename: "{sys}\sc.exe"; Parameters: "delete {#ServiceName}"; Flags: runhidden; RunOnceId: "DelSvc"

[Run]
; Post-install: offer to open the web console (ticked by default on the finish page).
; shellexec launches the URL in the default browser; nowait so setup closes immediately.
; The service may take a few seconds to come up.
Filename: "{#AppUrl}"; Description: "Open MySeliaSan in your browser"; Flags: postinstall shellexec skipifsilent nowait

[Code]
var
  AdminPassword: string;
  IsFreshInstall: Boolean;
  RngState: Cardinal;
  { Finish-page widgets that surface the one-time admin password so the operator can
    copy it rather than retype a 16-char string by eye. Created lazily. }
  PwLabel: TNewStaticText;
  PwEdit: TNewEdit;
  CopyBtn: TNewButton;

// Inno's built-in Random() draws from Delphi's global RandSeed, which the script cannot
// seed — Randomize is not exposed — so an unseeded Random() would hand every install the
// identical admin password. GetTickCount (imported below) seeds a self-contained
// generator instead so each install gets its own.
function GetTickCount: DWord; external 'GetTickCount@kernel32.dll stdcall';

// NextRandom returns a value in [0, Range) from a seeded LCG (glibc constants). The high
// bits are used (shr 16) because an LCG's low bits cycle short.
function NextRandom(Range: Integer): Integer;
var
  v: Integer;
begin
  RngState := RngState * 1103515245 + 12345;
  v := (RngState shr 16) and $7FFF;
  Result := v mod Range;
end;

procedure RunHidden(const Cmd, Params: string);
var
  rc: Integer;
begin
  Exec(Cmd, Params, '', SW_HIDE, ewWaitUntilTerminated, rc);
end;

// GenPassword builds a 16-char bootstrap admin password from an unambiguous charset (no
// O/0/I/l/1). It only ever seeds the first-run superadmin, which the app forces the
// operator to change on first login, so this is a one-time credential.
function GenPassword(): string;
var
  charset: string;
  i: Integer;
begin
  charset := 'ABCDEFGHJKMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789';
  Result := '';
  for i := 1 to 16 do
    Result := Result + charset[NextRandom(Length(charset)) + 1];
end;

procedure InitializeWizard();
begin
  // Seed from the install-time tick count (ms since boot), perturbed by the wall-clock
  // time of day, so the generated password differs per install.
  RngState := GetTickCount;
  RngState := RngState xor StrToIntDef(GetDateTimeString('hhnnss', #0, #0), 0);
  AdminPassword := GenPassword();
  // Fresh install == no existing database in the data dir. On an upgrade the superadmin
  // already exists and the seed password is ignored, so we must not claim a new one.
  IsFreshInstall := not FileExists(ExpandConstant('{commonappdata}\MySeliaSan\data\myseliasan.db'));
end;

procedure StopAndDeleteService();
begin
  RunHidden(ExpandConstant('{sys}\sc.exe'), 'stop {#ServiceName}');
  RunHidden(ExpandConstant('{sys}\sc.exe'), 'delete {#ServiceName}');
end;

// IsUpgrade is the [Tasks] Check for the reset-admin task: it is offered only on a
// reinstall over an existing data dir (where the admin password may be unknown).
function IsUpgrade(): Boolean;
begin
  Result := not IsFreshInstall;
end;

// ResetRequested reports whether the operator ticked "Reset the admin login" on an
// upgrade. On a fresh install there is nothing to reset (the task isn't shown).
function ResetRequested(): Boolean;
begin
  Result := IsUpgrade() and WizardIsTaskSelected('resetadmin');
end;

// ShowCredentials reports whether the finish page should reveal a bootstrap password: a
// fresh install always does; an upgrade only when a reset was asked.
function ShowCredentials(): Boolean;
begin
  Result := IsFreshInstall or ResetRequested();
end;

procedure InstallService();
var
  q, binPath, envData: string;
begin
  q := Chr(34);
  { binPath value: "<quoted exe>" -app myseliasan — the inner quotes handle the space in
    Program Files. }
  binPath := q + '\' + q + ExpandConstant('{app}\myseliasan.exe') + '\' + q + ' -app myseliasan' + q;
  RunHidden(ExpandConstant('{sys}\sc.exe'),
    'create {#ServiceName} binPath= ' + binPath + ' start= auto obj= LocalSystem DisplayName= ' + q + 'MySeliaSan fleet control plane' + q);
  RunHidden(ExpandConstant('{sys}\sc.exe'),
    'description {#ServiceName} ' + q + 'MySeliaSan - fleet control plane for MyMataSan camera nodes' + q);
  { Per-service environment (REG_MULTI_SZ, \0-delimited): home/data split + supervised
    restart so an in-app restart is relaunched. On a fresh install (or a requested admin
    reset) we also inject the generated bootstrap password, which resolveBootstrapPassword
    (services/rbac.go) reads via LOCAL_ADMIN_PASSWORD. It is a one-time, must-change
    credential. }
  envData := 'MYSELIASAN_HOME=' + ExpandConstant('{app}') + '\0' +
             'MYSELIASAN_DATA=' + ExpandConstant('{commonappdata}\MySeliaSan') + '\0' +
             'KOPIV2_SUPERVISED=1';
  if ShowCredentials() then
    envData := envData + '\0' + 'LOCAL_ADMIN_PASSWORD=' + AdminPassword;
  RunHidden(ExpandConstant('{sys}\reg.exe'),
    'add ' + q + 'HKLM\SYSTEM\CurrentControlSet\Services\{#ServiceName}' + q +
    ' /v Environment /t REG_MULTI_SZ /d ' + q + envData + q + ' /f');
  { On a requested reset over existing data, drop the one-shot marker the app consumes on
    next start to force the superadmin password to LOCAL_ADMIN_PASSWORD (the app deletes
    the marker first, so a later restart never re-runs it). }
  if ResetRequested() then
    SaveStringToFile(ExpandConstant('{commonappdata}\MySeliaSan\RESET_ADMIN'),
      'Admin reset requested by the installer. Safe to delete.' + #13#10, False);
  RunHidden(ExpandConstant('{sys}\sc.exe'),
    'failure {#ServiceName} reset= 86400 actions= restart/5000/restart/5000/restart/5000');
  RunHidden(ExpandConstant('{sys}\sc.exe'), 'start {#ServiceName}');
end;

// CredFilePath is where the one-time admin login is saved on a fresh install, so the
// operator can recover the generated password if they close the finish page before
// writing it down. It sits in the writable data root (admin-only) and is safe to delete
// once they have signed in and set their own password.
function CredFilePath(): string;
begin
  Result := ExpandConstant('{commonappdata}\MySeliaSan\INITIAL_ADMIN_LOGIN.txt');
end;

// WriteCredentialFile drops the bootstrap credentials to disk (best-effort). The password
// charset is alphanumeric, so no escaping is needed.
procedure WriteCredentialFile();
begin
  SaveStringToFile(CredFilePath(),
    'MySeliaSan - one-time administrator login' + #13#10 +
    '=========================================' + #13#10 + #13#10 +
    'Open:      {#AppUrl}' + #13#10 +
    'Username:  admin' + #13#10 +
    'Password:  ' + AdminPassword + #13#10 + #13#10 +
    'You will be asked to set your own password on first sign-in.' + #13#10 +
    'Delete this file once you have signed in.' + #13#10, False);
end;

// CopyToClipboard puts S on the clipboard without a trailing newline (the echo|set /p=
// trick). S is alphanumeric only, so it is safe on a command line.
procedure CopyToClipboard(const S: string);
var
  rc: Integer;
begin
  Exec(ExpandConstant('{cmd}'), '/C echo|set /p=' + S + '|clip', '', SW_HIDE, ewWaitUntilTerminated, rc);
end;

procedure CopyBtnClick(Sender: TObject);
begin
  CopyToClipboard(AdminPassword);
  // Highlight the value too (a visual confirm + lets the user Ctrl+C as a fallback).
  // Inno's Pascal Script does not expose TWinControl.SetFocus, so we only select — the
  // clipboard copy above is the actual action, so focus is not required.
  if PwEdit <> nil then
    PwEdit.SelectAll();
  CopyBtn.Caption := 'Copied';
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
    { On an upgrade, stop the running service so its exe isn't locked. }
    StopAndDeleteService()
  else if CurStep = ssPostInstall then
  begin
    InstallService();
    { Save the generated login so it is recoverable if the finish page is missed — on a
      fresh install or a requested admin reset. }
    if ShowCredentials() then
      WriteCredentialFile();
  end;
end;

// Rewrite the finish page into a "what next" guide: the URL to open, the login to use,
// and the one setting that must be right before adopting nodes.
procedure CurPageChanged(CurPageID: Integer);
begin
  if CurPageID = wpFinished then
  begin
    // Give the multi-line guidance room and push the "open browser" checkbox below it so
    // the two never overlap (Inno sizes the label for short text).
    WizardForm.FinishedLabel.AutoSize := False;
    WizardForm.FinishedLabel.WordWrap := True;
    if ShowCredentials() then
    begin
      WizardForm.FinishedLabel.Height := ScaleY(150);
      if IsFreshInstall then
        WizardForm.FinishedLabel.Caption :=
          'MySeliaSan is installed and running as a Windows service.' + #13#10 + #13#10 +
          'Open the web console:  {#AppUrl}' + #13#10 +
          '(Your browser shows a one-time warning for the self-signed certificate on a LAN — choose "proceed".)' + #13#10 + #13#10 +
          'Sign in as  Username: admin  with the one-time password below (also saved to' + #13#10 +
          CredFilePath() + ').' + #13#10 +
          'Before adopting nodes, set pairing.parentBaseUrl in config.json to this host''s LAN URL.'
      else
        WizardForm.FinishedLabel.Caption :=
          'MySeliaSan has been updated and your admin login was reset.' + #13#10 + #13#10 +
          'Open the web console:  {#AppUrl}' + #13#10 + #13#10 +
          'Sign in as  Username: admin  with the new one-time password below (also saved to' + #13#10 +
          CredFilePath() + ').' + #13#10 +
          'You will be asked to set your own password on first sign-in. Your adopted nodes,' + #13#10 +
          'users and settings are unchanged.';

      // Selectable password field + one-click Copy, so the operator never has to retype
      // the 16-char string. Created once; reused if the page is revisited.
      if PwLabel = nil then
      begin
        PwLabel := TNewStaticText.Create(WizardForm);
        PwLabel.Parent := WizardForm.FinishedPage;
        PwLabel.Caption := 'Admin password:';

        PwEdit := TNewEdit.Create(WizardForm);
        PwEdit.Parent := WizardForm.FinishedPage;
        PwEdit.ReadOnly := True;

        CopyBtn := TNewButton.Create(WizardForm);
        CopyBtn.Parent := WizardForm.FinishedPage;
        CopyBtn.Caption := 'Copy';
        CopyBtn.OnClick := @CopyBtnClick;
      end;

      PwLabel.Left := WizardForm.FinishedLabel.Left;
      PwLabel.Top := WizardForm.FinishedLabel.Top + WizardForm.FinishedLabel.Height + ScaleY(6);
      PwLabel.Width := WizardForm.FinishedLabel.Width;

      PwEdit.Text := AdminPassword;
      PwEdit.Left := WizardForm.FinishedLabel.Left;
      PwEdit.Top := PwLabel.Top + PwLabel.Height + ScaleY(2);
      PwEdit.Width := ScaleX(190);

      CopyBtn.Left := PwEdit.Left + PwEdit.Width + ScaleX(8);
      CopyBtn.Top := PwEdit.Top - ScaleY(1);
      CopyBtn.Width := ScaleX(75);
      CopyBtn.Height := PwEdit.Height + ScaleY(2);
      CopyBtn.Caption := 'Copy';

      WizardForm.RunList.Top := PwEdit.Top + PwEdit.Height + ScaleY(12);
    end
    else
    begin
      // Upgrade: no new password, so hide the copy widgets if a prior page created them
      // and restore the plain layout.
      if PwLabel <> nil then PwLabel.Visible := False;
      if PwEdit <> nil then PwEdit.Visible := False;
      if CopyBtn <> nil then CopyBtn.Visible := False;
      WizardForm.FinishedLabel.Height := ScaleY(150);
      WizardForm.RunList.Top := WizardForm.FinishedLabel.Top + WizardForm.FinishedLabel.Height + ScaleY(8);
      WizardForm.FinishedLabel.Caption :=
        'MySeliaSan has been updated and the service restarted.' + #13#10 + #13#10 +
        'Open the web console:  {#AppUrl}' + #13#10 +
        'Sign in with your existing account. Your adopted nodes and settings are unchanged.' + #13#10 + #13#10 +
        'Locked out? Re-run this installer and tick "Reset the admin login" to set a' + #13#10 +
        'new one-time password.' + #13#10 + #13#10 +
        'Manage the app any time from the "MySeliaSan" group in the Start Menu.';
    end;
  end;
end;

// UninstallHasFlag reports whether a switch was passed on the uninstaller's command
// line (case-insensitive). Inno exposes no helper for this on the uninstall side, so
// the parameters are walked directly. The uninstaller re-launches itself for its
// second phase and carries its original command line across, so a flag survives.
function UninstallHasFlag(const Flag: string): Boolean;
var
  i: Integer;
begin
  Result := False;
  for i := 1 to ParamCount do
    if CompareText(ParamStr(i), Flag) = 0 then
    begin
      Result := True;
      Exit;
    end;
end;

// WantsCleanData / WantsKeepData read the operator's intent from two places:
//
//   1. A switch on the uninstaller's command line: /CLEANDATA or /KEEPDATA.
//   2. The KOPIV2_PURGE_DATA environment variable: "1" wipe, "0" keep.
//
// Both exist for a reason. The uninstaller copies itself to %TEMP% and relaunches for
// its second phase, and THIS code runs in that second process — the environment is
// inherited by a child process whatever else happens, while forwarding a custom switch
// across the relaunch is Inno's business, not ours. So the variable is the dependable
// path for scripted uninstalls and the switch is the convenient one for a human.
//
// KOPIV2_PURGE_DATA is deliberately the same knob the Linux packages read
// (deploy/nfpm/myseliasan-postremove.sh), so one variable means the same thing on both
// platforms.
function WantsCleanData(): Boolean;
begin
  Result := UninstallHasFlag('/CLEANDATA') or (GetEnv('KOPIV2_PURGE_DATA') = '1');
end;

function WantsKeepData(): Boolean;
begin
  Result := UninstallHasFlag('/KEEPDATA') or (GetEnv('KOPIV2_PURGE_DATA') = '0');
end;

// On uninstall, decide what happens to the writable data root (database, settings,
// at-rest key). The default is to KEEP it. That matters more here than on a node: the
// at-rest key encrypts the fleet CA key and pairing PSK, so destroying it means
// re-adopting every node. A tester wanting a clean first-run experience answers Yes.
//
// An explicit request makes the choice without asking, so an unattended uninstall
// (MDM, imaging, a CI rig) can pick either outcome instead of only ever keeping:
//
//   set KOPIV2_PURGE_DATA=1 & unins000.exe /VERYSILENT     erase everything
//   unins000.exe /VERYSILENT /CLEANDATA                    same, via a switch
//   unins000.exe /VERYSILENT /KEEPDATA                     keep (today's default)
//
// Keep wins over clean: if both somehow arrive, the non-destructive reading is the
// safe one. Uninstalling from Add/Remove Programs sets neither, so that path still
// prompts. Mirrors `myseliasan-uninstall --purge-data` on Linux.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  dataDir: string;
  wipe: Boolean;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    dataDir := ExpandConstant('{commonappdata}\MySeliaSan');
    if not DirExists(dataDir) then
      Exit;
    if WantsKeepData() then
      Exit;
    wipe := WantsCleanData();
    // No explicit instruction: a silent/unattended uninstall keeps data and never
    // prompts; an interactive one asks, defaulting to No.
    if not wipe then
    begin
      if UninstallSilent then
        Exit;
      wipe := MsgBox('Also delete all MySeliaSan data — the database, users, settings and the' + #13#10 +
                     'fleet encryption key — under' + #13#10 + dataDir + '?' + #13#10 + #13#10 +
                     'The encryption key protects the fleet CA and pairing key: deleting it means' + #13#10 +
                     'every adopted node must be re-adopted.' + #13#10 + #13#10 +
                     'Choose No to keep your data for a future reinstall. This cannot be undone.',
                     mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
    end;
    if wipe then
      DelTree(dataDir, True, True, True);
  end;
end;
