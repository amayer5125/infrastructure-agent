<#
    .SYNOPSIS
        This script runs all the unit tests for New Relic Infrastructure Agent
#>

$scriptPath = split-path -parent $MyInvocation.MyCommand.Definition
$workspace = "$scriptPath\..\.."

Write-Output "Checking Go..."
go version
if (-not $?)
{
    Write-Output "Can't find Go"
    exit -1
}

Write-Output "--- Setting gopath"
$env:GOPATH = go env GOPATH

Write-Output "--- Running tests"

go test -ldflags '-X github.com/newrelic/infrastructure-agent/pkg/integrations/execution/v4/integration.minimumIntegrationIntervalOverride=2s' $workspace\pkg\... $workspace\cmd\... $workspace\internal\... $workspace\test\...
if (-not $?)
{
    Write-Output "Failed running tests"
    exit -1
}

exit 0