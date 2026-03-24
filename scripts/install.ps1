# NerdyAgent Windows Installer
# Usage: .\install.ps1 -ServerURL https://your-server.com -Token your-token
param(
    [Parameter(Mandatory=$true)][string]$ServerURL,
    [Parameter(Mandatory=$true)][string]$Token,
    [string]$Version = "latest"
)
$ErrorActionPreference = 'Stop'
$InstallDir = "$env:ProgramFiles\NerdyAgent"
$ConfigDir  = "$env:ProgramData\NerdyAgent"
$ConfigFile = "$ConfigDir\config.json"
$BinaryURL  = "$ServerURL/downloads/nerdyagent-windows-amd64.exe"
$BinaryPath = "$InstallDir\nerdyagent.exe"
$ServiceName = "NerdyAgent"

Write-Host "[+] Installing NerdyAgent"
Write-Host "[+] Server: $ServerURL"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path $ConfigDir  | Out-Null

Write-Host "[+] Downloading agent from $BinaryURL"
Invoke-WebRequest -Uri $BinaryURL -OutFile $BinaryPath -UseBasicParsing

$config = @{
    serverURL       = $ServerURL
    enrollmentToken = $Token
    checkinEvery    = "60s"
    jobTimeoutSec   = 120
    outputMaxBytes  = 131072
} | ConvertTo-Json
Set-Content -Path $ConfigFile -Value $config

# Install as Windows Service
$existingSvc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existingSvc) {
    Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
    sc.exe delete $ServiceName | Out-Null
    Start-Sleep -Seconds 1
}
New-Service -Name $ServiceName -DisplayName "NerdyAgent RMM Agent" `
    -BinaryPathName "`"$BinaryPath`"" `
    -StartupType Automatic `
    -Description "NerdyAgent remote monitoring and management agent"
$env:NRMM_AGENT_CONFIG = $ConfigFile
Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName" `
    -Name "Environment" -Value "NRMM_AGENT_CONFIG=$ConfigFile"
Start-Service -Name $ServiceName
Write-Host "[+] NerdyAgent installed and started!"
Write-Host "[+] Check status: Get-Service NerdyAgent"
Write-Host "[+] View logs:    Get-EventLog -LogName Application -Source NerdyAgent -Newest 50"
