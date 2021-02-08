#Requires -RunAsAdministrator

$ErrorActionPreference = 'Stop'

# Print Powershell version. Some features e.g. enums are
# not supported by old (pre-v5) Powershell versions.
$PSVersionTable.PSVersion

$ROOT_WORK_DIR = "${env:TMP}\fleetspeak-build-" + (Get-Date).ToString('yyyyMMdd_HHmmss')

$VERSION_FILE = "..\..\VERSION"
$VERSION = (Get-Content $VERSION_FILE | Out-String).Trim()

$PKG_WIX_CONFIG = ("fleetspeak.wxs" | Resolve-Path)
$PKG_WIX_CONFIG_LIB = ("fleetspeak_lib.wxs" | Resolve-Path)
$PKG_WORK_DIR = "${ROOT_WORK_DIR}\fleetspeak-pkg"

function Build-BinaryPkg {
  <#
  .SYNOPSIS
    Builds an installer for the Fleetspeak executable.
  #>

  Write-Host "Building binary package."

  Set-Location $PKG_WORK_DIR

  # -sw1150 arg disables warning due to
  # https://github.com/oleg-shilo/wixsharp/issues/299
  & "C:\Program Files (x86)\WiX Toolset v3.11\bin\candle.exe" `
    $PKG_WIX_CONFIG `
    -arch x64 `
    -ext WixUtilExtension `
    "-dFLEETSPEAK_EXECUTABLE=${ROOT_WORK_DIR}\fleetspeak-client.exe" `
    "-dVERSION=$VERSION" `
    -sw1150 `
    -out "fleetspeak-client.wixobj"

  & "C:\Program Files (x86)\WiX Toolset v3.11\bin\candle.exe" `
    $PKG_WIX_CONFIG_LIB `
    -arch x64 `
    -ext WixUtilExtension `
    "-dFLEETSPEAK_EXECUTABLE=${ROOT_WORK_DIR}\fleetspeak-client.exe" `
    "-dVERSION=$VERSION" `
    -sw1150 `
    -out "fleetspeak-client-lib.wixobj"

  # -sw1076 arg disables warning due to 'AllowDowngrades' setting in Wix config.
  & "C:\Program Files (x86)\WiX Toolset v3.11\bin\light.exe" `
    "fleetspeak-client.wixobj" `
    "fleetspeak-client-lib.wixobj" `
    -ext WixUtilExtension `
    -sw1076 `
    -out "fleetspeak-client-${VERSION}.msi"

  if (!$?) {
    throw "Failed to build binary package."
  }
}

function Test-Installer {
  <#
  .SYNOPSIS
    Checks that a binary MSI can be installed and uninstalled.
  #>

  param (
    [Parameter(Mandatory)]
    [string]$Path
  )

  Write-Host "Testing installer at $Path."

  $process = (
    Start-Process msiexec `
      -Wait `
      -PassThru `
      -ArgumentList '/i',$Path,'/qn','/l*v','installation.log'
  )

  Write-Host 'Installation log:'
  Get-Content installation.log

  if ($process.ExitCode -ne 0) {
    throw 'Installation of the MSI failed.'
  }

  # Launch msiexec and wait for the uninstallation to finish.
  $process = (
    Start-Process msiexec `
      -Wait `
      -PassThru `
      -ArgumentList '/x',$Path,'/qn','/l*v','uninstallation.log'
  )

  Write-Host 'Uninstallation log:'
  Get-Content uninstallation.log

  if ($process.ExitCode -ne 0) {
    throw 'Uninstallation of the MSI failed.'
  }

  # Make sure the logs directory gets created on install, and left
  # intact on uninstall.
  Write-Host 'Fleetspeak log files:'
  Get-ChildItem "${env:SYSTEMROOT}\Temp\Fleetspeak"

  Remove-Item installation.log
  Remove-Item uninstallation.log
}

# Copy Fleetspeak executable to the root work dir and sign it.
New-Item -Type Directory -Path $ROOT_WORK_DIR | Out-Null
Copy-Item "..\..\fleetspeak-client.exe" `
  "${ROOT_WORK_DIR}\fleetspeak-client.exe"

New-Item -Type Directory -Path $PKG_WORK_DIR | Out-Null

Build-BinaryPkg

#Test-Installer "${PKG_WORK_DIR}\fleetspeak-client-${VERSION}.msi"
