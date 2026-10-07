# Run from the repository with PowerShell 7. This is a developer-machine check,
# not a substitute for an OS without development tools.
param(
  [Parameter(Mandatory)][ValidateSet('Install', 'Verify', 'Uninstall')][string]$Action,
  [string]$RunName = 'windows-installation'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows) { throw 'Windows is required.' }
if ($RunName -notmatch '^[a-zA-Z0-9-]+$') { throw 'Invalid run name.' }
$repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$localRoot = Join-Path $repository '.local'
$work = Join-Path $localRoot $RunName
$destination = Join-Path $work '中文 安装'
$receiptPath = Join-Path $work 'installation.json'

function Assert-SafeTarget {
  $resolved = [IO.Path]::GetFullPath($destination)
  if (-not $resolved.StartsWith($localRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Install/uninstall target escapes the workspace validation directory.'
  }
  # Reject junctions/symlinks in every existing ancestor, including the checkout.
  $ancestor = $resolved
  while ($ancestor) {
    if ((Test-Path -LiteralPath $ancestor) -and ((Get-Item -LiteralPath $ancestor).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
      throw 'Validation paths must not traverse a reparse point.'
    }
    $ancestor = [IO.Path]::GetDirectoryName($ancestor)
  }
}
function Get-InstalledJeval {
  foreach ($root in @('HKCU:/Software/Microsoft/Windows/CurrentVersion/Uninstall', 'HKCU:/Software/WOW6432Node/Microsoft/Windows/CurrentVersion/Uninstall', 'HKLM:/Software/Microsoft/Windows/CurrentVersion/Uninstall', 'HKLM:/Software/WOW6432Node/Microsoft/Windows/CurrentVersion/Uninstall')) {
    if (Test-Path $root) {
      Get-ChildItem $root | ForEach-Object {
        $entry = Get-ItemProperty $_.PSPath
        if ($entry.PSObject.Properties['DisplayName'] -and $entry.DisplayName -match '^jeval(?: |$)') { $entry }
      }
    }
  }
}
function Save-Receipt($value) {
  $value | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $receiptPath -Encoding utf8
}
Assert-SafeTarget
if ($Action -eq 'Install') {
  if (Test-Path -LiteralPath $work) { throw 'Use a fresh RunName; existing validation data is preserved.' }
  if (@(Get-InstalledJeval).Count) { throw 'An existing jeval installation must not be replaced by this check.' }
  if (@(Get-Process jeval -ErrorAction SilentlyContinue).Count) { throw 'Close jeval before installation validation.' }
  foreach ($shortcut in @((Join-Path ([Environment]::GetFolderPath('Desktop')) 'jeval.lnk'), (Join-Path ([Environment]::GetFolderPath('Programs')) 'jeval.lnk'))) {
    if (Test-Path -LiteralPath $shortcut) { throw 'An existing jeval shortcut must not be replaced by this check.' }
  }
  $package = Get-Content -LiteralPath (Join-Path $repository 'apps/desktop/package.json') -Raw | ConvertFrom-Json
  $installer = Join-Path $repository "release/jeval Setup $($package.version).exe"
  if (-not (Test-Path -LiteralPath $installer)) { throw 'Build the NSIS installer with npm run dist:win first.' }
  New-Item -ItemType Directory -Path $work -Force | Out-Null
  $receipt = [ordered]@{
    schemaVersion = 1; phase = 'installing'; version = $package.version
    checkedAt = [DateTime]::UtcNow.ToString('o'); installDirectory = $destination
    installerSha256 = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash.ToLowerInvariant()
    installerBytes = (Get-Item -LiteralPath $installer).Length
    signature = (Get-AuthenticodeSignature -LiteralPath $installer).Status.ToString()
    hostBuild = [Environment]::OSVersion.Version.ToString(); cleanEnvironment = $false
  }
  Save-Receipt $receipt
  # NSIS requires /D last and unquoted, even with spaces. No shell evaluation.
  $process = Start-Process -FilePath $installer -ArgumentList "/S /currentuser /D=$destination" -WindowStyle Hidden -PassThru -Wait
  if ($process.ExitCode -ne 0) { throw "Installer failed with exit code $($process.ExitCode); preserve the receipt for diagnosis." }
}
if ($Action -in @('Install', 'Verify')) {
  $receipt = Get-Content -LiteralPath $receiptPath -Raw | ConvertFrom-Json -AsHashtable
  if ($receipt.phase -notin @('installing', 'installed') -or $receipt.installDirectory -cne $destination) { throw 'Receipt does not identify this test installation.' }
  $entries = @(Get-InstalledJeval)
  $expectedUninstall = '"' + (Join-Path $destination 'Uninstall jeval.exe') + '" /currentuser'
  if ($entries.Count -ne 1 -or $entries[0].UninstallString -ine $expectedUninstall) { throw 'Unexpected registered uninstall target.' }
  if ($entries[0].DisplayVersion -ne $receipt.version) { throw 'Unexpected registered version.' }
  $files = [ordered]@{}
  foreach ($relative in @('jeval.exe', 'resources/app.asar', 'resources/engine/jeval-engine.exe', 'resources/engine/THIRD-PARTY-NOTICES.txt')) {
    $installed = (Get-FileHash -LiteralPath (Join-Path $destination $relative) -Algorithm SHA256).Hash.ToLowerInvariant()
    $built = (Get-FileHash -LiteralPath (Join-Path $repository "release/win-unpacked/$relative") -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($installed -ne $built) { throw "Installed file differs from directory package: $relative" }
    $files[$relative] = $installed
  }
  $receipt.phase = 'installed'
  $receipt.files = $files
  $receipt.uninstallerSha256 = (Get-FileHash -LiteralPath (Join-Path $destination 'Uninstall jeval.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
  Save-Receipt $receipt
  Write-Output "Installed and verified NSIS payload; receipt: $receiptPath"
} else {
  $receipt = Get-Content -LiteralPath $receiptPath -Raw | ConvertFrom-Json
  if ($receipt.phase -ne 'installed' -or $receipt.installDirectory -cne $destination) { throw 'Receipt does not identify a verified test installation.' }
  $entries = @(Get-InstalledJeval)
  $expectedUninstall = '"' + (Join-Path $destination 'Uninstall jeval.exe') + '" /currentuser'
  if ($entries.Count -ne 1 -or $entries[0].UninstallString -ine $expectedUninstall) { throw 'Refusing to uninstall a different installation.' }
  if (@(Get-Process jeval -ErrorAction SilentlyContinue).Count) { throw 'Close jeval before uninstalling the test copy.' }
  $uninstaller = Join-Path $destination 'Uninstall jeval.exe'
  if ((Get-FileHash -LiteralPath $uninstaller -Algorithm SHA256).Hash.ToLowerInvariant() -ne $receipt.uninstallerSha256) { throw 'Uninstaller changed since installation.' }
  # Hash only the explicitly designated synthetic test profile, never user data.
  $database = Join-Path $work '测试 资料/library.sqlite'
  $before = if (Test-Path -LiteralPath $database) { (Get-FileHash -LiteralPath $database -Algorithm SHA256).Hash } else { $null }
  Assert-SafeTarget
  $process = Start-Process -FilePath $uninstaller -ArgumentList "/S /currentuser _?=$destination" -WindowStyle Hidden -PassThru -Wait
  if ($process.ExitCode -ne 0) { throw "Uninstaller failed with exit code $($process.ExitCode)." }
  if ((Test-Path -LiteralPath (Join-Path $destination 'jeval.exe')) -or @(Get-InstalledJeval).Count) { throw 'Uninstall did not remove the test app and registration.' }
  if ($before -and (Get-FileHash -LiteralPath $database -Algorithm SHA256).Hash -ne $before) { throw 'The external test profile changed during uninstall.' }
  # _?= intentionally leaves the running uninstaller in place. Remove only that
  # verified file after it exits; do not recursively delete the test directory.
  Remove-Item -LiteralPath $uninstaller
  if (@(Get-ChildItem -LiteralPath $destination -Force).Count -eq 0) { Remove-Item -LiteralPath $destination }
  $receipt.phase = 'uninstalled'
  $receipt | Add-Member -NotePropertyName externalTestProfilePreserved -NotePropertyValue ([bool]$before)
  Save-Receipt $receipt
  Write-Output 'Test app and registration removed; validation receipt and external test profile retained.'
}
