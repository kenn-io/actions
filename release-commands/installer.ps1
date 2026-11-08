# Parse installer output without evaluating the script or its expressions.
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$tree = [System.Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(), [ref]$tokens, [ref]$errors)
if ($errors.Count) {
    [Console]::Error.WriteLine("line $($errors[0].Extent.StartLineNumber): $($errors[0].Message)")
    exit 1
}
$printers = @{ 'Write-Host' = $true; 'Write-Output' = $true; 'echo' = $true; 'Write-Error' = $true; 'Write-Warning' = $true; 'Write-Information' = $true }
$functions = $tree.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true)
do {
    $changed = $false
    foreach ($function in $functions) {
        if ($printers[$function.Name]) { continue }
        $calls = $function.Body.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] }, $true)
        if ($calls | Where-Object { $_.GetCommandName() -and $printers[$_.GetCommandName()] }) {
            $printers[$function.Name] = $true
            $changed = $true
        }
    }
} while ($changed)
$messages = @($tree.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] }, $true) | ForEach-Object {
    $call = $_
    if (!$call.GetCommandName() -or !$printers[$call.GetCommandName()]) { return }
    $named = if ($call.GetCommandName() -eq 'Write-Host') { [System.Management.Automation.Language.StaticParameterBinder]::BindCommand($call, $true).BoundParameters.GetEnumerator() | Where-Object Key -ne 'Object' | ForEach-Object { $_.Value.Value } }
    $parts = @(foreach ($element in $call.CommandElements | Select-Object -Skip 1) {
        if ($named -notcontains $element -and ($element -is [System.Management.Automation.Language.StringConstantExpressionAst] -or $element -is [System.Management.Automation.Language.ExpandableStringExpressionAst])) {
            $line = $element.Extent.StartLineNumber
            if ($element.Extent.Text.StartsWith('@')) { $line++ }
            [pscustomobject]@{ text = $element.Value; line = $line }
        }
    })
    if ($call.GetCommandName() -eq 'Write-Host' -and $parts.Count) {
        [pscustomobject]@{ text = $parts.text -join ' '; line = $parts[0].line }
    } else { $parts }
})
ConvertTo-Json -InputObject $messages -Compress
