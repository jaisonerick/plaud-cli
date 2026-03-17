$ErrorActionPreference = "Stop"

$repo = "jaisonerick/plaud-cli"
$installDir = "$env:LOCALAPPDATA\plaud"

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }

Write-Host "Detecting system: windows/$arch"

$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
$tag = $release.tag_name
Write-Host "Latest release: $tag"

$url = "https://github.com/$repo/releases/download/$tag/plaud-cli_windows_$arch.exe"

if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir | Out-Null
}

$dest = Join-Path $installDir "plaud.exe"
Write-Host "Downloading $url..."
Invoke-WebRequest -Uri $url -OutFile $dest

# Add to PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    $env:Path = "$env:Path;$installDir"
    Write-Host "Added $installDir to user PATH"
}

Write-Host "Done! Run 'plaud --help' to get started."
Write-Host "(You may need to restart your terminal for PATH changes to take effect)"
