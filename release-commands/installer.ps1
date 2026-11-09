# Read installer messages from the syntax tree without running the installer or evaluating its expressions.
$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$source = [Console]::In.ReadToEnd()
$tree = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$errors)
if ($errors.Count) {
    [Console]::Error.WriteLine("line $($errors[0].Extent.StartLineNumber): $($errors[0].Message)")
    exit 1
}
# Each printer's message parameter; its other parameters, such as -Tags or -ForegroundColor, print nothing.
$printers = @{ 'Write-Host' = 'Object'; 'Write-Output' = 'InputObject'; 'Write-Error' = 'Message'; 'Write-Warning' = 'Message'; 'Write-Information' = 'MessageData' }
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
    # A here-string's text starts on the line after its opening quote.
    if ($expression.StringConstantType -like '*HereString') { $line++ }
    if ($expression -is [System.Management.Automation.Language.StringConstantExpressionAst]) { return [pscustomobject]@{ Text = $expression.Value; Line = $line } }
    if ($expression -is [System.Management.Automation.Language.BinaryExpressionAst] -and $expression.Operator -eq 'Plus') {
        $left = @(StaticValues $expression.Left $environment)
        $right = @(StaticValues $expression.Right $environment)
        if ($left.Count -eq 1 -and $right.Count -eq 1) {
            $line = if ($left[0].Line -eq $expression.Left.Extent.StartLineNumber -and $right[0].Line -ne $expression.Right.Extent.StartLineNumber) { $right[0].Line } else { $left[0].Line }
            [pscustomobject]@{ Text = $left[0].Text + $right[0].Text; Line = $line }
        }
    }
    if ($expression -is [System.Management.Automation.Language.ExpandableStringExpressionAst]) {
        $value = $expression.Value
        foreach ($nested in $expression.NestedExpressions) {
            $parts = @(StaticValues $nested $environment)
            $value = $value.Replace($nested.Extent.Text, ($parts.Text -join ' '))
            if ($parts.Count) { $line = $parts[0].Line }
        }
        [pscustomobject]@{ Text = $value; Line = $line }
    }
}
# Output after -NoNewline continues on the same line, so it joins the next message.
$pending = $null
function Emit($text, $line, $noNewLine) {
    if ($script:pending) { $text, $line = ($script:pending.Text + $text), $script:pending.Line }
    $script:pending = [pscustomobject]@{ Text = $text; Line = $line }
    if (!$noNewLine) { $script:pending; $script:pending = $null }
}
function ReadCalls($root, $environment, $depth) {
    foreach ($call in $root.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] }, $true)) {
        for ($parent = $call.Parent; $parent -ne $root -and $parent -isnot [System.Management.Automation.Language.FunctionDefinitionAst]; $parent = $parent.Parent) {}
        if ($parent -ne $root) { continue }
        $name = $call.GetCommandName()
        if ($name -and $functions.ContainsKey($name)) {
            if ($depth -ge $functions.Count) { throw "recursive printer helper $name" }
            $function = $functions[$name]
            $parameters = @(@($function.Parameters) + @($function.Body.ParamBlock.Parameters) | Where-Object { $null -ne $_ })
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
            ReadCalls $function.Body $bound ($depth + 1)
            continue
        }
        if (!$name -or !$printers.ContainsKey($name)) { continue }
        $bound = [System.Management.Automation.Language.StaticParameterBinder]::BindCommand($call, $true).BoundParameters
        $parts = @(if ($bound.ContainsKey($printers[$name])) { StaticValues $bound[$printers[$name]].Value $environment })
        $line = if ($parts.Count) { $parts[0].Line } else { $call.Extent.StartLineNumber }
        if ($name -eq 'Write-Host') {
            # The built-in formatter receives only resolved text, never a source expression.
            $format = @{ Object = [string[]]$parts.Text; NoNewline = $bound.ContainsKey('NoNewline') -and $bound['NoNewline'].ConstantValue -ne $false }
            if ($bound.ContainsKey('Separator')) { $format.Separator = @(StaticValues $bound['Separator'].Value $environment).Text -join ' ' }
            $message = (Microsoft.PowerShell.Utility\Write-Host @format 6>&1).MessageData
            Emit $message.Message $line $message.NoNewLine
        } elseif ($name -eq 'Write-Output') {
            foreach ($part in $parts) { Emit $part.Text $part.Line $false }
        } elseif ($parts.Count) {
            Emit ($parts.Text -join ' ') $line $false
        }
    }
}
$messages = @(ReadCalls $tree @{} 0; if ($pending) { $pending })
ConvertTo-Json -InputObject $messages -Compress
