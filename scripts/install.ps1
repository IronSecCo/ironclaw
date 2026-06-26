# IronClaw binary installer (Windows, PowerShell).
#
#   irm https://raw.githubusercontent.com/IronSecCo/ironclaw/main/scripts/install.ps1 | iex
#
# Resolves a release from GitHub, downloads the Windows archive, verifies its
# SHA-256 checksum, and installs ironctl.exe + ironclaw-controlplane.exe.
#
# Tunables (environment):
#   IRONCLAW_VERSION   release tag to install, e.g. v0.1.66   (default: latest)
#   IRONCLAW_BINDIR    install directory   (default: %LOCALAPPDATA%\IronClaw\bin)
#   IRONCLAW_REPO      owner/name of the GitHub repo          (default: IronSecCo/ironclaw)
#   GITHUB_TOKEN       optional; raises the GitHub API rate limit

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo    = if ($env:IRONCLAW_REPO)    { $env:IRONCLAW_REPO }    else { "IronSecCo/ironclaw" }
$version = if ($env:IRONCLAW_VERSION) { $env:IRONCLAW_VERSION } else { "latest" }
$bindir  = if ($env:IRONCLAW_BINDIR)  { $env:IRONCLAW_BINDIR }  else { Join-Path $env:LOCALAPPDATA "IronClaw\bin" }

if (-not [Environment]::Is64BitOperatingSystem) { throw "unsupported architecture (need 64-bit)" }
$target = "windows_amd64"
$headers = @{ "User-Agent" = "ironclaw-install" }
if ($env:GITHUB_TOKEN) { $headers["Authorization"] = "Bearer $($env:GITHUB_TOKEN)" }

if ($version -eq "latest") {
  $api = "https://api.github.com/repos/$repo/releases/latest"
} else {
  $api = "https://api.github.com/repos/$repo/releases/tags/$version"
}
Write-Host "==> Resolving release ($version)"
$rel = Invoke-RestMethod -Uri $api -Headers $headers

$asset = $rel.assets | Where-Object { $_.name -like "*_$target.zip" } | Select-Object -First 1
if (-not $asset) { throw "no asset for $target in release '$version' — see https://github.com/$repo/releases" }
$sums = $rel.assets | Where-Object { $_.name -eq "SHA256SUMS" } | Select-Object -First 1
Write-Host "==> Asset: $($asset.name)"

# In token mode download via the asset API URL with an octet-stream Accept header,
# so private-repo assets (which 404 on the public browser URL) download correctly.
$useApi = [bool]$env:GITHUB_TOKEN
$dlHeaders = @{} + $headers
if ($useApi) { $dlHeaders["Accept"] = "application/octet-stream" }
$assetUrl = if ($useApi) { $asset.url } else { $asset.browser_download_url }

$tmp = Join-Path $env:TEMP ("ironclaw_" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $zip = Join-Path $tmp $asset.name
  Write-Host "==> Downloading $($asset.name)"
  Invoke-WebRequest -Uri $assetUrl -OutFile $zip -Headers $dlHeaders

  if ($sums) {
    $sumsFile = Join-Path $tmp "SHA256SUMS"
    $sumsUrl = if ($useApi) { $sums.url } else { $sums.browser_download_url }
    Invoke-WebRequest -Uri $sumsUrl -OutFile $sumsFile -Headers $dlHeaders
    $line = Select-String -Path $sumsFile -Pattern ([regex]::Escape($asset.name)) | Select-Object -First 1
    if ($line) {
      $want = ($line.Line -split '\s+')[0].ToLower()
      $got  = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
      if ($want -ne $got) { throw "checksum mismatch for $($asset.name) (want $want, got $got)" }
      Write-Host "==> Checksum OK"
    }
  }

  Expand-Archive -Path $zip -DestinationPath $tmp -Force
  New-Item -ItemType Directory -Force -Path $bindir | Out-Null
  foreach ($b in @("ironctl.exe", "ironclaw-controlplane.exe")) {
    $src = Join-Path $tmp $b
    if (Test-Path $src) {
      Copy-Item -Force $src (Join-Path $bindir $b)
      Write-Host "==> Installed $(Join-Path $bindir $b)"
    } else {
      Write-Warning "$b not in archive — skipping"
    }
  }
} finally {
  Remove-Item -Recurse -Force $tmp
}

# Add the install dir to the user PATH if it isn't already there.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ';') -notcontains $bindir) {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$bindir", "User")
  Write-Host "==> Added $bindir to your user PATH (restart the terminal to pick it up)"
}
Write-Host "==> Done. Run: ironctl --version"
Write-Host ""
Write-Host "==> Note: this installs the host binaries (control plane + ironctl) and runs --dev"
Write-Host "    natively. A real agent sandbox needs Linux: gVisor (runsc) is Linux-only and the"
Write-Host "    Docker fallback uses a Unix socket native Windows does not expose. To run agents,"
Write-Host "    install inside WSL2 (wsl --install -d Ubuntu, then run install.sh in the distro)."
Write-Host "    See https://github.com/IronSecCo/ironclaw#windows-via-wsl2"
