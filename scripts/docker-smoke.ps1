[CmdletBinding()]
param(
    [string]$Image = "multispeed:local",
    [int]$Port = 18787
)

$ErrorActionPreference = "Stop"
$containerName = "multispeed-smoke-$([Guid]::NewGuid().ToString('N').Substring(0, 12))"
$temporaryBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$workDirectory = [IO.Path]::GetFullPath((Join-Path $temporaryBase "multispeed-smoke-$([Guid]::NewGuid().ToString('N'))"))
$dataDirectory = Join-Path $workDirectory "data"
$fakeCLI = Join-Path $workDirectory "librespeed-cli"
$curlImage = "curlimages/curl:8.16.0"

function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    $output = & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
    return $output
}

function Wait-Ready {
    for ($attempt = 1; $attempt -le 60; $attempt++) {
        try {
            Invoke-SmokeHTTP -Method GET -Path "/api/v1/readyz" | Out-Null
            return
        } catch {
            if ($attempt -eq 60) {
                & docker logs $containerName
                throw
            }
            Start-Sleep -Seconds 1
        }
    }
}

function Invoke-SmokeHTTP {
    param(
        [string]$Method,
        [string]$Path,
        [string]$Body = ""
    )
    $arguments = @("run", "--rm", "--network", "container:$containerName")
    if ($Body -ne "") {
        $bodyFile = Join-Path $workDirectory "request-$([Guid]::NewGuid().ToString('N')).json"
        [IO.File]::WriteAllText($bodyFile, $Body, [Text.UTF8Encoding]::new($false))
        $arguments += @("--mount", "type=bind,src=$bodyFile,dst=/tmp/request.json,readonly")
    }
    $arguments += @(
        $curlImage, "--silent", "--show-error", "--fail-with-body", "--request", $Method,
        "--header", "Host:127.0.0.1:$Port"
    )
    if ($Body -ne "") {
        $arguments += @("--header", "Content-Type:application/json", "--data-binary", "@/tmp/request.json")
    }
    $arguments += "http://127.0.0.1:$Port$Path"
    return (Invoke-Docker @arguments | Out-String).Trim()
}

function Get-SmokeHTTPStatus {
    param(
        [string]$Method,
        [string]$Path,
        [string]$Body
    )
    $bodyFile = Join-Path $workDirectory "request-$([Guid]::NewGuid().ToString('N')).json"
    [IO.File]::WriteAllText($bodyFile, $Body, [Text.UTF8Encoding]::new($false))
    $arguments = @(
        "run", "--rm", "--network", "container:$containerName",
        "--mount", "type=bind,src=$bodyFile,dst=/tmp/request.json,readonly",
        $curlImage, "--silent", "--show-error", "--output", "/dev/null", "--write-out", "%{http_code}",
        "--request", $Method, "--header", "Host:127.0.0.1:$Port",
        "--header", "Content-Type:application/json", "--data-binary", "@/tmp/request.json",
        "http://127.0.0.1:$Port$Path"
    )
    return [int](Invoke-Docker @arguments | Out-String).Trim()
}

try {
    New-Item -ItemType Directory -Path $dataDirectory -Force | Out-Null
    $fixture = @(
        '#!/bin/sh',
        'if [ "${1:-}" = "--version" ]; then',
        "  printf '%s\n' 'librespeed-cli v1.0.13+multispeed.dns2.xnet055 smoke fixture'",
        '  exit 0',
        'fi',
        'printf ''%s\n'' ''[{"timestamp":"2026-01-01T00:00:00Z","server":{"name":"Local smoke fixture","url":"http://127.0.0.1"},"client":{"ip":"203.0.113.10"},"bytes_sent":62500000,"bytes_received":125000000,"ping":8.25,"jitter":0.75,"upload":50,"download":100,"share":""}]'''
    ) -join "`n"
    [IO.File]::WriteAllText($fakeCLI, "$fixture`n", [Text.UTF8Encoding]::new($false))

    $routeLine = (Invoke-Docker run --rm --network host --entrypoint ip $Image -4 route get 1.1.1.1 | Select-Object -First 1)
    $interfaceMatch = [regex]::Match($routeLine, '(?:^|\s)dev\s+(\S+)')
    $sourceMatch = [regex]::Match($routeLine, '(?:^|\s)src\s+(\S+)')
    if (!$interfaceMatch.Success -or !$sourceMatch.Success) {
        throw "Could not determine the container's default IPv4 interface and source from: $routeLine"
    }
    $interfaceName = $interfaceMatch.Groups[1].Value
    $sourceIP = $sourceMatch.Groups[1].Value

    Invoke-Docker run --detach --name $containerName --init --platform linux/amd64 --network host `
        --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges:true --read-only `
        --tmpfs '/tmp:rw,noexec,nosuid,nodev,size=64m' `
        --mount "type=bind,src=$dataDirectory,dst=/data" `
        --mount "type=bind,src=$fakeCLI,dst=/opt/multispeed/providers/librespeed-cli,readonly" `
        --env "APP_LISTEN_ADDR=127.0.0.1:$Port" --env APP_DATA_DIR=/data `
        --env LIBRESPEED_BINARY=/opt/multispeed/providers/librespeed-cli `
        --env ACCEPT_OOKLA_EULA=false $Image | Out-Null

    Wait-Ready
    Invoke-SmokeHTTP -Method GET -Path "/api/v1/healthz" | Out-Null
    $frontend = Invoke-SmokeHTTP -Method GET -Path "/"
    if ($frontend -notmatch 'MultiSpeed') {
        throw "Embedded frontend did not contain MultiSpeed branding"
    }

    $initialSettings = Invoke-SmokeHTTP -Method GET -Path "/api/v1/settings" | ConvertFrom-Json
    if ($initialSettings.ooklaEulaAccepted -ne $false) { throw "Ookla EULA acceptance was not false initially" }

    $unconfirmedStatus = Get-SmokeHTTPStatus -Method PUT -Path "/api/v1/settings/ookla-eula" -Body '{"accepted":true,"confirmed":false}'
    if ($unconfirmedStatus -ne 422) { throw "Unconfirmed Ookla EULA acceptance returned HTTP $unconfirmedStatus instead of 422" }
    $rejectedSettings = Invoke-SmokeHTTP -Method GET -Path "/api/v1/settings" | ConvertFrom-Json
    if ($rejectedSettings.ooklaEulaAccepted -ne $false) { throw "Unconfirmed Ookla EULA acceptance changed persisted state" }

    $acceptedSettings = Invoke-SmokeHTTP -Method PUT -Path "/api/v1/settings/ookla-eula" -Body '{"accepted":true,"confirmed":true}' | ConvertFrom-Json
    if ($acceptedSettings.ooklaEulaAccepted -ne $true) { throw "Confirmed Ookla EULA acceptance was not persisted" }

    if ((Invoke-Docker inspect --format '{{.HostConfig.NetworkMode}}' $containerName) -ne 'host') { throw "container is not using host networking" }
    if ((Invoke-Docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' $containerName) -ne 'true') { throw "root filesystem is not read-only" }
    if ((Invoke-Docker inspect --format '{{.Config.User}}' $containerName) -eq '0') { throw "container is running as root" }
    if ((Invoke-Docker inspect --format '{{json .HostConfig.CapDrop}}' $containerName) -notmatch 'ALL') { throw "all capabilities were not dropped" }
    if ((Invoke-Docker inspect --format '{{json .HostConfig.SecurityOpt}}' $containerName) -notmatch 'no-new-privileges') { throw "no-new-privileges is missing" }

    $task = @{
        name = "Docker smoke LibreSpeed"
        description = "Deterministic local provider fixture"
        enabled = $false
        provider = "librespeed"
        cronExpression = "0 * * * *"
        timezone = "UTC"
        serverSelectionMode = "automatic"
        interfaceName = $interfaceName
        sourceIp = $sourceIP
        ipFamily = "ipv4"
        timeoutSeconds = 30
        routeValidation = "required"
    } | ConvertTo-Json
    $created = Invoke-SmokeHTTP -Method POST -Path "/api/v1/tasks" -Body $task | ConvertFrom-Json
    $run = Invoke-SmokeHTTP -Method POST -Path "/api/v1/tasks/$($created.id)/run" -Body '{}' | ConvertFrom-Json

    $result = $null
    for ($attempt = 1; $attempt -le 90; $attempt++) {
        $result = Invoke-SmokeHTTP -Method GET -Path "/api/v1/results/$($run.id)" | ConvertFrom-Json
        if ($result.status -eq 'succeeded') { break }
        if ($result.status -in @('failed', 'skipped', 'cancelled')) {
            & docker logs $containerName
            $diagnostics = $result | ConvertTo-Json -Depth 10 -Compress
            throw "Smoke result entered terminal status $($result.status): $diagnostics"
        }
        if ($attempt -eq 90) { throw "Smoke result did not complete" }
        Start-Sleep -Seconds 1
    }

    if ($result.downloadBitsPerSecond -ne 100000000) { throw "Unexpected normalized download: $($result.downloadBitsPerSecond)" }
    if ($result.uploadBitsPerSecond -ne 50000000) { throw "Unexpected normalized upload: $($result.uploadBitsPerSecond)" }

    $configurationJSON = Invoke-SmokeHTTP -Method GET -Path "/api/v1/config/export"
    if ($configurationJSON -notmatch '"format":"multispeed-config"' -or $configurationJSON -notmatch [regex]::Escape($created.id)) {
        throw "Configuration export did not contain the expected format and task"
    }
    if ($configurationJSON -match 'ooklaEula') { throw "Configuration export leaked Ookla EULA state" }
    $configuration = $configurationJSON | ConvertFrom-Json
    $configuration.tasks[0].name = "Docker smoke restored"
    $configurationBody = $configuration | ConvertTo-Json -Depth 20 -Compress
    $imported = Invoke-SmokeHTTP -Method POST -Path "/api/v1/config/import" -Body $configurationBody | ConvertFrom-Json
    if ($imported.taskCount -ne 1 -or $imported.routeProfileCount -ne 0 -or $imported.settingsUpdated -ne $true) {
        throw "Unexpected configuration import result: $($imported | ConvertTo-Json -Compress)"
    }
    $restoredTask = Invoke-SmokeHTTP -Method GET -Path "/api/v1/tasks/$($created.id)" | ConvertFrom-Json
    if ($restoredTask.name -ne "Docker smoke restored") { throw "Configuration import did not replace the task" }
    $postImportSettings = Invoke-SmokeHTTP -Method GET -Path "/api/v1/settings" | ConvertFrom-Json
    if ($postImportSettings.ooklaEulaAccepted -ne $true) { throw "Configuration import changed Ookla EULA acceptance" }

    $database = Get-Item -LiteralPath (Join-Path $dataDirectory "multispeed.db")
    if ($database.Length -le 0) { throw "SQLite database was not created" }
    Invoke-Docker restart $containerName | Out-Null
    Wait-Ready
    $persisted = Invoke-SmokeHTTP -Method GET -Path "/api/v1/results/$($run.id)" | ConvertFrom-Json
    if ($persisted.status -ne 'succeeded') { throw "Result did not survive restart" }
    $persistedSettings = Invoke-SmokeHTTP -Method GET -Path "/api/v1/settings" | ConvertFrom-Json
    if ($persistedSettings.ooklaEulaAccepted -ne $true) { throw "Ookla EULA acceptance did not survive restart" }

    Write-Output "Docker smoke test passed for $Image with config roundtrip and EULA persistence (result $($run.id))."
} finally {
    & docker rm --force $containerName *> $null
    if (Test-Path -LiteralPath $workDirectory) {
        $resolved = [IO.Path]::GetFullPath($workDirectory)
        if (!$resolved.StartsWith($temporaryBase, [StringComparison]::OrdinalIgnoreCase) -or
            !([IO.Path]::GetFileName($resolved)).StartsWith('multispeed-smoke-', [StringComparison]::Ordinal)) {
            throw "Refusing to remove unexpected smoke directory: $resolved"
        }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
