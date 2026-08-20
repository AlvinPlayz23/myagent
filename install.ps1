#Requires -Version 5.1
<#
.SYNOPSIS
  Install myagent from GitHub Releases.

.DESCRIPTION
  Downloads the correct archive for Windows (amd64/arm64) from
  https://github.com/AlvinPlayz23/myagent/releases and installs
  myagent.exe to $InstallDir (default: $env:LOCALAPPDATA\myagent\bin).

.USAGE
  # latest release, default location:
  irm https://raw.githubusercontent.com/AlvinPlayz23/myagent/main/install.ps1 | iex

  # pinned version / custom dir:
  irm https://raw.githubusercontent.com/AlvinPlayz23/myagent/main/install.ps1 | iex; Install-MyAgent -Version v0.1.0
  # or
  powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/AlvinPlayz23/myagent/main/install.ps1 | iex"

.PARAMETER Version
  Git tag to install (e.g. v0.1.0) or "latest" (default).

.PARAMETER InstallDir
  Directory to install myagent.exe into.

.PARAMETER NoPathUpdate
  Don't modify the user's PATH.
#>
param(
  [string]$Version = "latest",
  [string]$InstallDir = "",
  [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"
$Repo = "AlvinPlayz23/myagent"
$Binary = "myagent"
$GITHUB_DOWNLOAD = "https://github.com/$Repo/releases/download"

function Install-MyAgent {
  param(
    [string]$Version = "latest",
    [string]$InstallDir = "",
    [switch]$NoPathUpdate
  )

  # 1. Resolve install directory
  if (-not $InstallDir -or $InstallDir -eq "") {
    if ($env:LOCALAPPDATA) { $InstallDir = Join-Path $env:LOCALAPPDATA "myagent\bin" }
    else { $InstallDir = Join-Path $env:USERPROFILE ".myagent\bin" }
  }

  # 2. Resolve "latest" -> real tag via GitHub API (fallback to redirect check)
  $Tag = $Version
  if (-not $Tag -or $Tag -eq "latest") {
    Write-Host "Checking GitHub for latest release..." -ForegroundColor Cyan
    try {
      $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "myagent-installer" } -TimeoutSec 15
      $Tag = $release.tag_name
      if (-not $Tag) { throw "empty tag_name" }
      Write-Host "Found latest: $Tag" -ForegroundColor Green
    } catch {
      Write-Warning "API lookup failed ($_), trying redirect URL..."
      # unauthenticated API is 60 req/hr; fallback: let download URL use /latest/download/ redirect
      # we still need a tag for filename, so fail with helpful message
      throw "Unable to resolve latest version (GitHub API rate-limited or offline). Try pinning: Install-MyAgent -Version v0.1.0 . Error: $_"
    }
  }
  if ($Tag -notlike "v*") { Write-Warning "Tag '$Tag' doesn't start with 'v' - releases are tagged vX.Y.Z, this may 404" }

  # 3. Detect arch
  $archRaw = $env:PROCESSOR_ARCHITECTURE
  if ($env:PROCESSOR_ARCHITEW6432) { $archRaw = $env:PROCESSOR_ARCHITEW6432 }
  $Arch = "amd64"
  if ($archRaw -like "*ARM64*" -or $archRaw -like "*ARM*") { $Arch = "arm64" }
  elseif ($archRaw -like "*86*") { $Arch = "amd64" }
  Write-Host "Detected arch: $archRaw -> $Arch" -ForegroundColor DarkGray

  $Os = "windows"
  $TARBALL = "myagent_${Tag}_${Os}_${Arch}.zip"
  $TARBALL_URL = "$GITHUB_DOWNLOAD/$Tag/$TARBALL"
  $CHECKSUM_URL = "$GITHUB_DOWNLOAD/$Tag/checksums.txt"

  Write-Host "Downloading $TARBALL_URL" -ForegroundColor Cyan

  $tmpDir = Join-Path $env:TEMP "myagent-install-$(Get-Random)"
  New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
  $zipPath = Join-Path $tmpDir $TARBALL
  $checksumPath = Join-Path $tmpDir "checksums.txt"

  try {
    # Use TLS 1.2
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    # Download archive
    Invoke-WebRequest -Uri $TARBALL_URL -OutFile $zipPath -UseBasicParsing -Headers @{ "User-Agent" = "myagent-installer" }

    # Download checksums.txt (optional but verify if present)
    $hasChecksum = $true
    try {
      Invoke-WebRequest -Uri $CHECKSUM_URL -OutFile $checksumPath -UseBasicParsing -Headers @{ "User-Agent" = "myagent-installer" } -TimeoutSec 15
    } catch {
      Write-Warning "Could not download checksums.txt, skipping verification: $_"
      $hasChecksum = $false
    }

    if ($hasChecksum -and (Test-Path $checksumPath)) {
      $expectedLine = Select-String -Path $checksumPath -Pattern ([regex]::Escape($TARBALL)) | Select-Object -First 1
      if ($expectedLine) {
        $expectedHash = ($expectedLine.Line -split '\s+')[0].ToLower()
        $actualHash = (Get-FileHash -Path $zipPath -Algorithm SHA256).Hash.ToLower()
        if ($expectedHash -ne $actualHash) {
          throw "Checksum mismatch for $TARBALL`nExpected: $expectedHash`nActual:   $actualHash"
        }
        Write-Host "Checksum verified" -ForegroundColor Green
      } else {
        Write-Warning "Checksum entry not found for $TARBALL, skipping"
      }
    }

    # Extract
    Write-Host "Extracting to $tmpDir" -ForegroundColor Cyan
    # Expand-Archive overwrites on pwsh 6+, -Force for 5.1
    Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

    # Find binary (archive may contain myagent.exe at root or nested)
    $binSrc = Get-ChildItem -Path $tmpDir -Recurse -Filter "$Binary.exe" | Select-Object -First 1
    if (-not $binSrc) { throw "Binary $Binary.exe not found in archive" }

    if (-not (Test-Path $InstallDir)) { New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null }

    $binDest = Join-Path $InstallDir "$Binary.exe"
    Copy-Item -Path $binSrc.FullName -Destination $binDest -Force
    Write-Host "Installed $binDest" -ForegroundColor Green

    # PATH update
    if (-not $NoPathUpdate) {
      $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
      if ($userPath -notlike "*$InstallDir*") {
        Write-Host "Adding $InstallDir to user PATH..." -ForegroundColor Cyan
        $newPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path += ";$InstallDir"
        Write-Host "Added to PATH. Restart your shell to use '$Binary' from anywhere." -ForegroundColor Yellow
      } else {
        Write-Host "$InstallDir already on PATH" -ForegroundColor DarkGray
      }
    }

    Write-Host "`nRun '$Binary --help' to get started" -ForegroundColor Green
    # verify
    try { & $binDest --version } catch {}

  } finally {
    # cleanup
    if (Test-Path $tmpDir) { Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue }
  }
}

# Auto-run when invoked via irm | iex (not when dot-sourced)
if ($MyInvocation.InvocationName -ne '.' -and $MyInvocation.Line -notmatch '^\s*\.\s') {
  Install-MyAgent -Version $Version -InstallDir $InstallDir -NoPathUpdate:$NoPathUpdate
}
