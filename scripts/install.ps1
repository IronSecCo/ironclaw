# IronClaw binary installer (Windows, PowerShell).
#
#   irm https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.ps1 | iex
#
# Resolves a release from GitHub, downloads the Windows archive, verifies its
# SHA-256 checksum, and installs ironctl.exe + ironclaw-controlplane.exe.
#
# Tunables (environment):
#   IRONCLAW_VERSION   release tag to install, e.g. v0.1.66   (default: latest)
#   IRONCLAW_BINDIR    install directory   (default: %LOCALAPPDATA%\IronClaw\bin)
#   IRONCLAW_REPO      owner/name of the GitHub repo          (default: nivardsec/ironclaw)
#   GITHUB_TOKEN       optional; raises the GitHub API rate limit

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocol]::Tls12

$repo    = if ($env:IRONCLAW_REPO)    { $env:IRONCLAW_REPO }    else { "nivardsec/ironclaw" }
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

$tmp = Join-Path $env:TEMP ("ironclaw_" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $zip = Join-Path $tmp $asset.name
  Write-Host "==> Downloading $($asset.name)"
  Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip -Headers $headers

  if ($sums) {
    $sumsFile = Join-Path $tmp "SHA256SUMS"
    Invoke-WebRequest -Uri $sums.browser_download_url -OutFile $sumsFile -Headers $headers
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
