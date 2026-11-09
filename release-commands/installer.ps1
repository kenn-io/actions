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
function StaticString($expression) {
    if ($expression -is [System.Management.Automation.Language.ParenExpressionAst] -and $expression.Pipeline.PipelineElements.Count -eq 1) { $expression = $expression.Pipeline.PipelineElements[0].Expression }
    if ($expression -is [System.Management.Automation.Language.StringConstantExpressionAst]) { return $expression.Value }
    if ($expression -is [System.Management.Automation.Language.BinaryExpressionAst] -and $expression.Operator -eq 'Plus') { $left, $right = (StaticString $expression.Left), (StaticString $expression.Right) }
    if ($null -ne $left -and $null -ne $right) { $left + $right }
}
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
    $bound = if ($call.GetCommandName() -eq 'Write-Host') { [System.Management.Automation.Language.StaticParameterBinder]::BindCommand($call, $true).BoundParameters }
    $named = if ($bound) { $bound.GetEnumerator() | Where-Object Key -ne 'Object' | ForEach-Object { $_.Value.Value } }
    $parts = @(foreach ($element in $call.CommandElements | Select-Object -Skip 1) {
        if ($named -contains $element) { continue }
        $items = if ($element -is [System.Management.Automation.Language.ArrayExpressionAst]) {
            foreach ($statement in $element.SubExpression.Statements) {
                if ($statement -is [System.Management.Automation.Language.PipelineAst] -and $statement.PipelineElements.Count -eq 1 -and $statement.PipelineElements[0] -is [System.Management.Automation.Language.CommandExpressionAst]) { $statement.PipelineElements[0].Expression }
            }
        } else { $element }
        $items = foreach ($item in $items) { if ($item -is [System.Management.Automation.Language.ArrayLiteralAst]) { $item.Elements } else { $item } }
        foreach ($element in $items) {
            $value = StaticString $element
            if ($element -is [System.Management.Automation.Language.ExpandableStringExpressionAst]) { $value = $element.Value }
            if ($null -ne $value) {
                $line = $element.Extent.StartLineNumber
                if ($element.Extent.Text.StartsWith('@')) { $line++ }
                [pscustomobject]@{ text = $value; line = $line }
            }
        }
    })
    if ($call.GetCommandName() -eq 'Write-Host' -and $parts.Count) {
        $separator = if ($bound.ContainsKey('Separator')) { $bound['Separator'].ConstantValue } else { ' ' }
        [pscustomobject]@{ text = $parts.text -join $separator; line = $parts[0].line }
    } else { $parts }
})
ConvertTo-Json -InputObject $messages -Compress
