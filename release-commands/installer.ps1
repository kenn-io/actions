# Parse installer output without evaluating the script or its expressions.
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$source = [Console]::In.ReadToEnd()
$tree = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$errors)
if ($errors.Count) {
    [Console]::Error.WriteLine("line $($errors[0].Extent.StartLineNumber): $($errors[0].Message)")
    exit 1
}
$printers = @('Write-Host', 'Write-Output', 'echo', 'Write-Error', 'Write-Warning', 'Write-Information')
$functions = @{}
foreach ($function in $tree.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true)) { $functions[$function.Name] = $function }
function StaticValues($expression, $environment) {
    if ($expression -is [System.Management.Automation.Language.ParenExpressionAst]) { return StaticValues $expression.Pipeline $environment }
    if ($expression -is [System.Management.Automation.Language.SubExpressionAst] -or $expression -is [System.Management.Automation.Language.ArrayExpressionAst]) { return StaticValues $expression.SubExpression $environment }
    if ($expression -is [System.Management.Automation.Language.StatementBlockAst]) {
        foreach ($statement in $expression.Statements) { StaticValues $statement $environment }
        return
    }
    if ($expression -is [System.Management.Automation.Language.PipelineAst] -and $expression.PipelineElements.Count -eq 1) { return StaticValues $expression.PipelineElements[0] $environment }
    if ($expression -is [System.Management.Automation.Language.CommandExpressionAst]) { return StaticValues $expression.Expression $environment }
    if ($expression -is [System.Management.Automation.Language.ArrayLiteralAst]) {
        foreach ($element in $expression.Elements) { StaticValues $element $environment }
        return
    }
    if ($expression -is [System.Management.Automation.Language.VariableExpressionAst]) {
        if ($environment.ContainsKey($expression.VariablePath.UserPath)) { $environment[$expression.VariablePath.UserPath] }
        return
    }
    $line = $expression.Extent.StartLineNumber
    if ($expression -is [System.Management.Automation.Language.StringConstantExpressionAst]) { return [pscustomobject]@{ text = $expression.Value; line = $line } }
    if ($expression -is [System.Management.Automation.Language.BinaryExpressionAst] -and $expression.Operator -eq 'Plus') {
        $left = @(StaticValues $expression.Left $environment)
        $right = @(StaticValues $expression.Right $environment)
        if ($left.Count -eq 1 -and $right.Count -eq 1) {
            $line = if ($left[0].line -eq $expression.Left.Extent.StartLineNumber -and $right[0].line -ne $expression.Right.Extent.StartLineNumber) { $right[0].line } else { $left[0].line }
            [pscustomobject]@{ text = $left[0].text + $right[0].text; line = $line }
        }
    }
    if ($expression -is [System.Management.Automation.Language.ExpandableStringExpressionAst]) {
        $value = $expression.Value
        foreach ($nested in $expression.NestedExpressions) {
            $parts = @(StaticValues $nested $environment)
            $value = $value.Replace($nested.Extent.Text, ($parts.text -join ' '))
            if ($parts.Count) { $line = $parts[0].line }
        }
        [pscustomobject]@{ text = $value; line = $line }
    }
}
function ReadCalls($root, $environment, $depth, $statement, $end, $inherited, $after = [int]::MaxValue) {
    foreach ($call in $root.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] }, $true)) {
        for ($parent = $call.Parent; $parent -and $parent -ne $root -and $parent -isnot [System.Management.Automation.Language.FunctionDefinitionAst]; $parent = $parent.Parent) {}
        if ($parent -is [System.Management.Automation.Language.FunctionDefinitionAst]) { continue }
        $name = $call.GetCommandName()
        if (!$name -or (!$functions.ContainsKey($name) -and $name -notin $printers)) { continue }
        $outer, $last = if ($statement -ge 0) { $statement, $end } else { foreach ($offset in $call.Extent.StartOffset, $call.Extent.EndOffset) { [System.Text.Encoding]::UTF8.GetByteCount($source.Substring(0, $offset)) } }
        $code = @($tokens | Where-Object { $_.Extent.EndOffset -le [Math]::Min($after, $call.Extent.StartOffset) -and $_.Kind -notin 'Comment', 'NewLine', 'LineContinuation' })[-1]
        $comment, $inherited = ($inherited + $(foreach ($token in @($tokens | Where-Object { $statement -ge 0 -and $_.Kind -eq 'NewLine' -and $_.Extent.StartOffset -ge $code.Extent.EndOffset -and $_.Extent.EndOffset -le $call.Extent.StartOffset })[0]) { $source.Substring($token.Extent.EndOffset, $call.Extent.StartOffset - $token.Extent.EndOffset) })), ''
        if ($functions.ContainsKey($name)) {
            if ($depth -ge $functions.Count) { throw "recursive printer helper $name" }
            $function = $functions[$name]
            $parameters = @($function.Parameters) + @($function.Body.ParamBlock.Parameters) | Where-Object { $null -ne $_ }
            $bound = @{}
            $position = 0
            for ($index = 1; $index -lt $call.CommandElements.Count; $index++) {
                $element = $call.CommandElements[$index]
                if ($element -is [System.Management.Automation.Language.CommandParameterAst]) {
                    $parameter = $element.ParameterName
                    if ($element.Argument) { $element = $element.Argument } else { $element = $call.CommandElements[++$index] }
                } else {
                    while ($position -lt $parameters.Count -and $bound.ContainsKey($parameters[$position].Name.VariablePath.UserPath)) { $position++ }
                    if ($position -ge $parameters.Count) { continue }
                    $parameter = $parameters[$position++].Name.VariablePath.UserPath
                }
                $bound[$parameter] = @(StaticValues $element $environment)
            }
            ReadCalls $function.Body $bound ($depth + 1) $outer $last $comment | ForEach-Object { $_; $after = $call.Extent.EndOffset }
            continue
        }
        $bound = if ($name -eq 'Write-Host') { [System.Management.Automation.Language.StaticParameterBinder]::BindCommand($call, $true).BoundParameters }
        $named = if ($bound) { $bound.GetEnumerator() | Where-Object Key -ne 'Object' | ForEach-Object { $_.Value.Value } }
        $parts = @(foreach ($element in $call.CommandElements | Select-Object -Skip 1) {
            if ($named -contains $element) { continue }
            StaticValues $element $environment
        })
        if ($name -eq 'Write-Host' -and $parts.Count) {
            $separator = if ($bound.ContainsKey('Separator')) { (StaticValues $bound['Separator'].Value $environment).text -join ' ' } else { ' ' }
            $parts = @([pscustomobject]@{ text = $parts.text -join $separator; line = $parts[0].line })
        }
        foreach ($part in $parts) { [pscustomobject]@{ text = $part.text; line = $part.line; statement = $outer; end = $last; comment = $comment }; $comment, $after = '', $call.Extent.EndOffset }
    }
}
$messages = @(ReadCalls $tree @{} 0 -1 -1 '')
ConvertTo-Json -InputObject $messages -Compress
