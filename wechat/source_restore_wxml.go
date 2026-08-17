package wechat

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

type wxmlAttribute struct {
	Name  string
	Value *string
}

type wxmlElement struct {
	Tag         string
	Attrs       []wxmlAttribute
	Children    []*wxmlElement
	Text        bool
	TextValue   string
	Generator   *ast.FunctionLiteral
	Parent      *wxmlElement
	Placeholder bool
}

func restoreWXMLSources(runtimeCode []string) (map[string]string, []string) {
	result, warnings := restoreGlassEaselWXMLSources(runtimeCode)
	aliases := extractWXMLPathAliases(runtimeCode)

	for _, code := range runtimeCode {
		for gwxName, gwxSource := range extractGwxFunctionSources(code) {
			xPool, err := extractWXMLPathPool(gwxSource)
			if err != nil || len(xPool) == 0 {
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skip %s WXML environment: %v", gwxName, err))
				}
				continue
			}
			zPools, zWarnings := extractWXMLZPools(gwxSource)
			warnings = append(warnings, zWarnings...)
			mFunctions := extractNamedFunctionAssignments(gwxSource, "m")
			mappings := extractWXMLEntryMappings(gwxSource)
			for _, mapping := range mappings {
				if mapping.Index < 0 || mapping.Index >= len(xPool) {
					warnings = append(warnings, fmt.Sprintf("skip WXML entry %s: path pool index %d is out of range", mapping.FunctionName, mapping.Index))
					continue
				}
				functionSource, ok := mFunctions[mapping.FunctionName]
				if !ok {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: missing render function %s", xPool[mapping.Index], mapping.FunctionName))
					continue
				}
				function, err := parseJavaScriptFunctionLiteral(functionSource)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: parse render function: %v", xPool[mapping.Index], err))
					continue
				}
				zName := findWXMLZFunctionName(function)
				zValues, ok := zPools[zName]
				if !ok || len(zValues) == 0 {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: missing expression pool %s", xPool[mapping.Index], zName))
					continue
				}
				relativePath, err := resolveWXMLEntryPath(gwxName, xPool[mapping.Index], aliases)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: %v", xPool[mapping.Index], err))
					continue
				}
				rootName := findWXMLReturnName(function)
				if rootName == "" {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: render function has no root return", relativePath))
					continue
				}
				root := &wxmlElement{}
				names := map[string]*wxmlElement{rootName: root}
				currentZ := zName
				if err := analyzeWXMLStatements(function.Body.List, zPools, &currentZ, zName, xPool, names, map[string]*wxmlElement{rootName: root}); err != nil {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: %v", relativePath, err))
					continue
				}
				content := renderWXMLChildren(root.Children, 0)
				if strings.TrimSpace(content) == "" {
					warnings = append(warnings, fmt.Sprintf("skip WXML path %q: renderer produced no nodes", relativePath))
					continue
				}
				result[relativePath] = content
			}
		}
	}
	return result, warnings
}

type wxmlEntryMapping struct {
	Index        int
	FunctionName string
}

var wxmlEntryMappingPattern = regexp.MustCompile("e_\\s*\\[\\s*x\\s*\\[\\s*(\\d+)\\s*\\]\\s*\\]\\s*=\\s*\\{\\s*f\\s*:\\s*(m[A-Za-z0-9_$]+)")

func extractWXMLEntryMappings(code string) []wxmlEntryMapping {
	matches := wxmlEntryMappingPattern.FindAllStringSubmatch(code, -1)
	result := make([]wxmlEntryMapping, 0, len(matches))
	for _, match := range matches {
		index, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		result = append(result, wxmlEntryMapping{Index: index, FunctionName: match[2]})
	}
	return result
}

func extractGwxFunctionSources(code string) map[string]string {
	result := make(map[string]string)
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "$gwx")
		if index < 0 {
			break
		}
		index += offset
		if index > 0 && isJavaScriptIdentifierPart(code[index-1]) {
			offset = index + len("$gwx")
			continue
		}
		cursor := index + len("$gwx")
		for cursor < len(code) && isJavaScriptIdentifierPart(code[cursor]) {
			cursor++
		}
		name := code[index:cursor]
		cursor = skipJavaScriptTrivia(code, cursor)
		if cursor >= len(code) || code[cursor] != '=' {
			offset = cursor
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+1)
		if !isJavaScriptIdentifierAt(code, cursor, "function") {
			offset = cursor + 1
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+len("function"))
		if cursor < len(code) && isJavaScriptIdentifierPart(code[cursor]) {
			offset = cursor + 1
			continue
		}
		if cursor >= len(code) || code[cursor] != '(' {
			offset = cursor + 1
			continue
		}
		parameterEnd, err := findMatchingJavaScriptDelimiter(code, cursor, '(', ')')
		if err != nil {
			break
		}
		bodyOpen := skipJavaScriptTrivia(code, parameterEnd+1)
		if bodyOpen >= len(code) || code[bodyOpen] != '{' {
			offset = parameterEnd + 1
			continue
		}
		bodyClose, err := findMatchingJavaScriptDelimiter(code, bodyOpen, '{', '}')
		if err != nil {
			break
		}
		result[name] = code[index : bodyClose+1]
		offset = bodyClose + 1
	}
	return result
}

func extractNamedFunctionAssignments(code, prefix string) map[string]string {
	result := make(map[string]string)
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "var")
		if index < 0 {
			break
		}
		index += offset
		if !isJavaScriptIdentifierAt(code, index, "var") {
			offset = index + 3
			continue
		}
		cursor := skipJavaScriptTrivia(code, index+3)
		nameStart := cursor
		for cursor < len(code) && isJavaScriptIdentifierPart(code[cursor]) {
			cursor++
		}
		name := code[nameStart:cursor]
		if name == "" || !strings.HasPrefix(name, prefix) {
			offset = index + 3
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor)
		if cursor >= len(code) || code[cursor] != '=' {
			offset = cursor
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+1)
		if !isJavaScriptIdentifierAt(code, cursor, "function") {
			offset = cursor + 1
			continue
		}
		functionStart := cursor
		cursor = skipJavaScriptTrivia(code, cursor+len("function"))
		if cursor < len(code) && isJavaScriptIdentifierPart(code[cursor]) {
			offset = cursor + 1
			continue
		}
		if cursor >= len(code) || code[cursor] != '(' {
			offset = cursor + 1
			continue
		}
		parameterEnd, err := findMatchingJavaScriptDelimiter(code, cursor, '(', ')')
		if err != nil {
			break
		}
		bodyOpen := skipJavaScriptTrivia(code, parameterEnd+1)
		if bodyOpen >= len(code) || code[bodyOpen] != '{' {
			offset = parameterEnd + 1
			continue
		}
		bodyClose, err := findMatchingJavaScriptDelimiter(code, bodyOpen, '{', '}')
		if err != nil {
			break
		}
		result[name] = code[functionStart : bodyClose+1]
		offset = bodyClose + 1
	}
	return result
}

func extractWXMLPathPool(code string) ([]string, error) {
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "var")
		if index < 0 {
			break
		}
		index += offset
		if !isJavaScriptIdentifierAt(code, index, "var") {
			offset = index + 3
			continue
		}
		cursor := skipJavaScriptTrivia(code, index+3)
		if !isJavaScriptIdentifierAt(code, cursor, "x") {
			offset = cursor + 1
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+1)
		if cursor >= len(code) || code[cursor] != '=' {
			offset = cursor + 1
			continue
		}
		cursor = skipJavaScriptTrivia(code, cursor+1)
		end := findJavaScriptExpressionEnd(code, cursor)
		value, err := parseStaticJavaScriptExpression(code[cursor:end])
		if err != nil {
			return nil, err
		}
		items, ok := value.([]staticJavaScriptValue)
		if !ok {
			return nil, fmt.Errorf("path pool is %T, not an array", value)
		}
		result := make([]string, len(items))
		hasWXML := false
		for i, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("path pool item %d is %T, not a string", i, item)
			}
			result[i] = text
			if strings.HasSuffix(strings.ToLower(text), ".wxml") {
				hasWXML = true
			}
		}
		if hasWXML {
			return result, nil
		}
		offset = end + 1
	}
	return nil, fmt.Errorf("no WXML path pool found")
}

func extractWXMLPathAliases(runtimeCode []string) map[string]string {
	result := make(map[string]string)
	for _, code := range runtimeCode {
		for _, assignment := range extractWxAppCodeAssignments(code, "runtime") {
			if !strings.EqualFold(path.Ext(strings.ReplaceAll(assignment.Key, string(rune(92)), "/")), ".wxml") {
				continue
			}
			gwxIndex := strings.Index(assignment.Expression, "$gwx")
			if gwxIndex < 0 {
				continue
			}
			cursor := gwxIndex
			for cursor < len(assignment.Expression) && isJavaScriptIdentifierPart(assignment.Expression[cursor]) {
				cursor++
			}
			gwxName := assignment.Expression[gwxIndex:cursor]
			literals := extractJavaScriptStringLiterals(assignment.Expression)
			if len(literals) == 0 {
				continue
			}
			relative := strings.TrimPrefix(strings.ReplaceAll(literals[0], string(rune(92)), "/"), "./")
			result[gwxName+"\x00"+relative] = assignment.Key
		}
	}
	return result
}

func extractJavaScriptStringLiterals(code string) []string {
	var result []string
	for i := 0; i < len(code); {
		next, skipped := skipJavaScriptToken(code, i)
		if skipped {
			if code[i] == '\'' || code[i] == '"' {
				value, _, ok := parseJavaScriptString(code, i)
				if ok {
					result = append(result, value)
				}
			}
			i = next
			continue
		}
		i++
	}
	return result
}

func resolveWXMLEntryPath(gwxName, pathValue string, aliases map[string]string) (string, error) {
	relative := strings.TrimPrefix(strings.ReplaceAll(pathValue, string(rune(92)), "/"), "./")
	if alias, ok := aliases[gwxName+"\x00"+relative]; ok {
		return normalizeCompiledSourcePath(alias)
	}
	pluginID := pluginIDFromGwxName(gwxName)
	if pluginID != "" {
		return normalizeCompiledSourcePath("plugin-private://" + pluginID + "/" + relative)
	}
	return normalizeCompiledSourcePath(relative)
}

func pluginIDFromGwxName(name string) string {
	name = strings.TrimPrefix(name, "$gwx_")
	if len(name) < 18 {
		return ""
	}
	candidate := strings.ToLower(name[:18])
	if reWxId.MatchString(candidate) {
		return candidate
	}
	return ""
}

func extractWXMLZPools(code string) (map[string][]string, []string) {
	result := make(map[string][]string)
	var warnings []string
	for name, source := range extractNamedFunctionSources(code, "gz$gwx") {
		values, err := restoreWXMLZFunction(source)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip expression pool %s: %v", name, err))
			continue
		}
		result[name] = values
	}
	return result, warnings
}

func restoreWXMLZFunction(source string) ([]string, error) {
	program, err := parser.ParseFile(nil, "wxml-z.js", source, 0)
	if err != nil {
		return nil, err
	}
	variables := make(map[string]staticJavaScriptValue)
	var rawValues []staticJavaScriptValue
	seen := make(map[string]bool)
	walkJavaScriptAST(program, func(node ast.Node) {
		switch item := node.(type) {
		case *ast.VariableDeclaration:
			for _, binding := range item.List {
				name, ok := binding.Target.(*ast.Identifier)
				if !ok || binding.Initializer == nil {
					continue
				}
				if value, valueErr := evaluateStaticJavaScriptExpression(binding.Initializer, variables); valueErr == nil {
					variables[name.Name.String()] = value
				}
			}
		case *ast.CallExpression:
			callee, ok := item.Callee.(*ast.Identifier)
			if !ok || callee.Name.String() != "Z" || len(item.ArgumentList) == 0 {
				return
			}
			key := strconv.FormatUint(uint64(item.Idx0()), 10)
			if seen[key] {
				return
			}
			seen[key] = true
			if value, referenced := wxmlZPoolReference(item.ArgumentList[0], rawValues); referenced {
				rawValues = append(rawValues, value)
			} else if value, valueErr := evaluateStaticJavaScriptExpression(item.ArgumentList[0], variables); valueErr == nil {
				rawValues = append(rawValues, value)
			} else {
				// Keep the operation's position even when its expression is
				// too dynamic for the static evaluator. Every Z call occupies
				// one stable index in the runtime pool; dropping it would shift
				// all following attributes and conditions to the wrong value.
				rawValues = append(rawValues, nil)
			}
		}
	})
	if len(rawValues) == 0 {
		return nil, fmt.Errorf("no static Z operations found")
	}
	result := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		if operation, ok := value.([]staticJavaScriptValue); ok && len(operation) >= 2 && operation[0] == "11182016" {
			if nested, nestedOK := operation[1].([]staticJavaScriptValue); nestedOK {
				value = nested
			}
		}
		restored, err := restoreWXMLExpression(value, false)
		if err != nil {
			// Preserve the pool slot when an operation code is not yet
			// understood. Empty output is safer than shifting later indexes.
			restored = ""
		}
		result = append(result, restored)
	}
	return result, nil
}

func wxmlZPoolReference(expression ast.Expression, values []staticJavaScriptValue) (staticJavaScriptValue, bool) {
	bracket, ok := expression.(*ast.BracketExpression)
	if !ok {
		return nil, false
	}
	identifier, ok := bracket.Left.(*ast.Identifier)
	if !ok || identifier.Name.String() != "z" {
		return nil, false
	}
	index := wxmlNumberExpression(bracket.Member)
	if index < 0 || index >= len(values) {
		return nil, true
	}
	return values[index], true
}

func restoreWXMLExpression(value staticJavaScriptValue, withScope bool) (string, error) {
	if value == nil {
		return "", nil
	}
	operation, ok := value.([]staticJavaScriptValue)
	if !ok {
		switch number := value.(type) {
		case float64:
			return strconv.FormatFloat(number, 'f', -1, 64), nil
		case string:
			return number, nil
		case bool:
			return strconv.FormatBool(number), nil
		case staticUndefined:
			return "undefined", nil
		default:
			return "", fmt.Errorf("unexpected WXML operation %T", value)
		}
	}
	code, arguments, err := wxmlOperationParts(operation)
	if err != nil {
		return "", err
	}
	switch code {
	case 1:
		if len(arguments) < 1 {
			return "", fmt.Errorf("WXML variable operation is incomplete")
		}
		return wxmlScope(wxmlJavaScriptValue(arguments[0]), withScope), nil
	case 3:
		if len(arguments) < 1 {
			return "", fmt.Errorf("WXML literal operation is incomplete")
		}
		return wxmlJavaScriptValue(arguments[0]), nil
	case 11:
		var builder strings.Builder
		for _, child := range arguments {
			restored, err := restoreWXMLExpression(child, withScope)
			if err != nil {
				return "", err
			}
			builder.WriteString(restored)
		}
		return builder.String(), nil
	case 2:
		if len(arguments) < 2 {
			return "", fmt.Errorf("binary WXML operation is incomplete")
		}
		operator, ok := arguments[0].(string)
		if !ok {
			return "", fmt.Errorf("WXML operator is %T", arguments[0])
		}
		get := func(index int) (string, error) {
			if index >= len(arguments) {
				return "", fmt.Errorf("WXML operator %s is missing an operand", operator)
			}
			value, err := restoreWXMLExpression(arguments[index], true)
			if err != nil {
				return "", err
			}
			if nested, nestedOK := arguments[index].([]staticJavaScriptValue); nestedOK {
				if nestedCode, nestedArguments, nestedErr := wxmlOperationParts(nested); nestedErr == nil && nestedCode == 2 && len(nestedArguments) > 0 {
					childOperator, _ := nestedArguments[0].(string)
					if wxmlPrecedence(operator, len(arguments)) > wxmlPrecedence(childOperator, len(nestedArguments)) {
						value = "(" + value + ")"
					}
				}
			}
			return value, nil
		}
		if operator == "?:" && len(operation) >= 4 {
			test, err := get(1)
			if err != nil {
				return "", err
			}
			consequent, err := get(2)
			if err != nil {
				return "", err
			}
			alternate, err := get(3)
			if err != nil {
				return "", err
			}
			return wxmlScope(test+"?"+consequent+":"+alternate, withScope), nil
		}
		left, err := get(1)
		if err != nil {
			return "", err
		}
		if operator == "!" || operator == "~" || (operator == "-" && len(arguments) != 3) {
			return wxmlScope(operator+left, withScope), nil
		}
		right, err := get(2)
		if err != nil {
			return "", err
		}
		return wxmlScope(left+operator+right, withScope), nil
	case 4:
		if len(arguments) < 1 {
			return "", fmt.Errorf("WXML parenthesized operation is incomplete")
		}
		value, err := restoreWXMLExpression(arguments[0], true)
		if err != nil {
			return "", err
		}
		return wxmlScope(value, withScope), nil
	case 5:
		if len(arguments) == 0 {
			return "[]", nil
		}
		value, err := restoreWXMLExpression(arguments[0], true)
		if err != nil {
			return "", err
		}
		return wxmlScope("["+value+"]", withScope), nil
	case 6:
		if len(arguments) < 2 {
			return "", fmt.Errorf("member WXML operation is incomplete")
		}
		left, err := restoreWXMLExpression(arguments[0], true)
		if err != nil {
			return "", err
		}
		right, err := restoreWXMLExpression(arguments[1], true)
		if err != nil {
			return "", err
		}
		if wxmlOperationIsVariable(arguments[1]) {
			return wxmlScope(left+"["+right+"]", withScope), nil
		}
		if isSimpleWXMLIdentifier(right) {
			return wxmlScope(left+"."+right, withScope), nil
		}
		return wxmlScope(left+"["+right+"]", withScope), nil
	case 7:
		if len(arguments) < 1 {
			return "", fmt.Errorf("value WXML operation is incomplete")
		}
		inner, ok := arguments[0].([]staticJavaScriptValue)
		if !ok || len(inner) < 2 {
			return "", fmt.Errorf("value WXML operation has an invalid reference")
		}
		if number, numberOK := inner[0].(float64); numberOK && int(number) == 3 {
			return wxmlScope(wxmlJavaScriptValue(inner[1]), withScope), nil
		}
		return "", fmt.Errorf("unsupported value WXML operation")
	default:
		return restoreWXMLExpressionTail(code, arguments, withScope)
	}
}

func wxmlOperationParts(operation []staticJavaScriptValue) (int, []staticJavaScriptValue, error) {
	if len(operation) == 0 {
		return 0, nil, fmt.Errorf("empty WXML operation")
	}
	if number, err := staticNumber(operation[0]); err == nil {
		if number != float64(int(number)) {
			return 0, nil, fmt.Errorf("WXML operation code is not an integer: %v", number)
		}
		return int(number), operation[1:], nil
	}
	header, ok := operation[0].([]staticJavaScriptValue)
	if !ok || len(header) == 0 {
		return 0, nil, fmt.Errorf("WXML operation code is %T", operation[0])
	}
	number, err := staticNumber(header[0])
	if err != nil || number != float64(int(number)) {
		return 0, nil, fmt.Errorf("WXML operation code is %T", header[0])
	}
	arguments := operation[1:]
	if len(header) > 1 {
		arguments = append([]staticJavaScriptValue{header[1]}, arguments...)
	}
	return int(number), arguments, nil
}

func restoreWXMLExpressionTail(code int, arguments []staticJavaScriptValue, withScope bool) (string, error) {
	switch code {
	case 8:
		if len(arguments) < 2 {
			return "", fmt.Errorf("object WXML operation is incomplete")
		}
		value, err := restoreWXMLExpression(arguments[1], true)
		if err != nil {
			return "", err
		}
		return wxmlScope("{"+wxmlJavaScriptValue(arguments[0])+":"+value+"}", withScope), nil
	case 9:
		if len(arguments) < 2 {
			return "", fmt.Errorf("merge WXML operation is incomplete")
		}
		left, err := restoreWXMLExpression(arguments[0], true)
		if err != nil {
			return "", err
		}
		right, err := restoreWXMLExpression(arguments[1], true)
		if err != nil {
			return "", err
		}
		return wxmlScope("{"+left+","+right+"}", withScope), nil
	case 10:
		if len(arguments) < 1 {
			return "", fmt.Errorf("spread WXML operation is incomplete")
		}
		value, err := restoreWXMLExpression(arguments[0], true)
		if err != nil {
			return "", err
		}
		return wxmlScope("..."+value, withScope), nil
	case 12:
		if len(arguments) < 2 {
			return "", fmt.Errorf("call WXML operation is incomplete")
		}
		array, err := restoreWXMLExpression(arguments[1], true)
		if err != nil {
			return "", err
		}
		function, err := restoreWXMLExpression(arguments[0], true)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(array, "[") && strings.HasSuffix(array, "]") {
			return wxmlScope(function+"("+strings.TrimSuffix(strings.TrimPrefix(array, "["), "]")+")", withScope), nil
		}
		return wxmlScope(function+".apply(null,"+array+")", withScope), nil
	default:
		return "", fmt.Errorf("unsupported WXML operation code %d", code)
	}
}

func wxmlOperationIsVariable(value staticJavaScriptValue) bool {
	operation, ok := value.([]staticJavaScriptValue)
	if !ok || len(operation) < 2 {
		return false
	}
	code, ok := operation[0].(float64)
	if !ok || int(code) != 7 {
		return false
	}
	inner, ok := operation[1].([]staticJavaScriptValue)
	if !ok || len(inner) < 2 {
		return false
	}
	innerCode, ok := inner[0].(float64)
	return ok && int(innerCode) == 3
}

func wxmlScope(value string, withScope bool) string {
	if withScope {
		return value
	}
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		return "{{(" + value + ")}}"
	}
	if strings.HasPrefix(value, "...") {
		return "{" + value + "}"
	}
	return "{{" + value + "}}"
}

func wxmlJavaScriptValue(value staticJavaScriptValue) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case staticUndefined:
		return "undefined"
	case nil:
		return "null"
	default:
		return fmt.Sprint(typed)
	}
}

func wxmlPrecedence(operator string, length int) int {
	switch operator {
	case "?:":
		return 4
	case "||":
		return 5
	case "&&":
		return 6
	case "|":
		return 7
	case "^":
		return 8
	case "&":
		return 9
	case "===", "==", "!=", "!==":
		return 10
	case ">=", "<=", ">", "<":
		return 11
	case "<<", ">>":
		return 12
	case "+", "-":
		return 13
	case "*", "/", "%":
		return 14
	case "!", "~":
		return 16
	default:
		return 0
	}
}

func isSimpleWXMLIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '_' || (i > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func analyzeWXMLStatements(statements []ast.Statement, zPools map[string][]string, currentZ *string, initialZ string, xPool []string, names map[string]*wxmlElement, fake map[string]*wxmlElement) error {
	for _, statement := range statements {
		switch item := statement.(type) {
		case *ast.VariableStatement:
			for _, binding := range item.List {
				name, ok := binding.Target.(*ast.Identifier)
				if !ok || binding.Initializer == nil {
					continue
				}
				if call, callOK := binding.Initializer.(*ast.CallExpression); callOK {
					callee := wxmlCalleeName(call.Callee)
					if strings.HasPrefix(callee, "gz$gwx") {
						*currentZ = callee
						continue
					}
					switch callee {
					case "_n":
						tag, ok := wxmlStringArgument(call, 0)
						if ok {
							names[name.Name.String()] = &wxmlElement{Tag: tag}
						}
					case "_v":
						names[name.Name.String()] = &wxmlElement{Tag: "block"}
					case "_o":
						value, err := wxmlPoolValue(zPools, *currentZ, wxmlNumberArgument(call, 0))
						if err != nil {
							return err
						}
						names[name.Name.String()] = &wxmlElement{Text: true, TextValue: value}
					case "_oz":
						value, err := wxmlPoolValue(zPools, *currentZ, wxmlNumberArgument(call, 1))
						if err != nil {
							return err
						}
						names[name.Name.String()] = &wxmlElement{Text: true, TextValue: value}
					case "_m", "_mz":
						element, err := wxmlElementFromMCall(call, zPools, *currentZ, callee == "_mz")
						if err != nil {
							return err
						}
						names[name.Name.String()] = element
					}
				} else if function, functionOK := binding.Initializer.(*ast.FunctionLiteral); functionOK {
					names[name.Name.String()] = &wxmlElement{Generator: function}
				}
			}
		case *ast.ExpressionStatement:
			if call, ok := item.Expression.(*ast.CallExpression); ok {
				if err := analyzeWXMLCall(call, zPools, currentZ, xPool, names, fake); err != nil {
					return err
				}
			}
		case *ast.IfStatement:
			if err := analyzeWXMLIf(item, zPools, currentZ, initialZ, xPool, names, fake); err != nil {
				return err
			}
		}
	}
	return nil
}

func analyzeWXMLCall(call *ast.CallExpression, zPools map[string][]string, currentZ *string, xPool []string, names map[string]*wxmlElement, fake map[string]*wxmlElement) error {
	callee := wxmlCalleeName(call.Callee)
	switch callee {
	case "_":
		if len(call.ArgumentList) >= 2 {
			parent, parentOK := wxmlIdentifierName(call.ArgumentList[0])
			child, childOK := wxmlIdentifierName(call.ArgumentList[1])
			if parentOK && childOK {
				wxmlPushChild(names, fake, parent, child)
			}
		}
	case "_r":
		if len(call.ArgumentList) >= 3 {
			node, nodeOK := wxmlIdentifierName(call.ArgumentList[0])
			attr, attrOK := wxmlStringArgument(call, 1)
			if nodeOK && attrOK {
				value, err := wxmlPoolValue(zPools, *currentZ, wxmlNumberArgument(call, 2))
				if err != nil {
					return err
				}
				wxmlSetAttribute(wxmlResolveElement(names, fake, node), attr, value)
			}
		}
	case "_rz":
		if len(call.ArgumentList) >= 4 {
			node, nodeOK := wxmlIdentifierName(call.ArgumentList[1])
			attr, attrOK := wxmlStringArgument(call, 2)
			if nodeOK && attrOK {
				value, err := wxmlPoolValue(zPools, *currentZ, wxmlNumberArgument(call, 3))
				if err != nil {
					return err
				}
				wxmlSetAttribute(wxmlResolveElement(names, fake, node), attr, value)
			}
		}
	case "_2", "_2z":
		return analyzeWXMLLoop(call, callee == "_2z", zPools, currentZ, xPool, names, fake)
	case "_ic":
		if len(call.ArgumentList) >= 6 {
			parent, parentOK := wxmlIdentifierName(call.ArgumentList[5])
			source, sourceOK := wxmlPathArgument(call.ArgumentList[0], xPool)
			if parentOK && sourceOK {
				wxmlPushElement(names, fake, parent, &wxmlElement{Tag: "include", Attrs: []wxmlAttribute{{Name: "src", Value: stringPointer(source)}}})
			}
		}
	case "_ai":
		if len(call.ArgumentList) >= 5 {
			source, sourceOK := wxmlPathArgument(call.ArgumentList[1], xPool)
			if sourceOK {
				parent, parentOK := wxmlIdentifierName(call.ArgumentList[4])
				if !parentOK {
					for candidate := range fake {
						parent = candidate
						parentOK = true
						break
					}
				}
				if parentOK {
					wxmlPushElement(names, fake, parent, &wxmlElement{Tag: "import", Attrs: []wxmlAttribute{{Name: "src", Value: stringPointer(source)}}})
				}
			}
		}
	}
	return nil
}

func findWXMLZFunctionName(function *ast.FunctionLiteral) string {
	if function == nil {
		return ""
	}
	var result string
	walkJavaScriptAST(function, func(node ast.Node) {
		if result != "" {
			return
		}
		call, ok := node.(*ast.CallExpression)
		if !ok {
			return
		}
		callee := wxmlCalleeName(call.Callee)
		if strings.HasPrefix(callee, "gz$gwx") {
			result = callee
		}
	})
	return result
}

func findWXMLReturnName(function *ast.FunctionLiteral) string {
	if function == nil || function.Body == nil {
		return ""
	}
	for _, statement := range function.Body.List {
		if name := wxmlReturnNameFromStatement(statement); name != "" {
			return name
		}
	}
	return ""
}

func wxmlReturnNameFromStatement(statement ast.Statement) string {
	switch item := statement.(type) {
	case *ast.ReturnStatement:
		return wxmlIdentifierFromExpression(item.Argument)
	case *ast.BlockStatement:
		for _, nested := range item.List {
			if name := wxmlReturnNameFromStatement(nested); name != "" {
				return name
			}
		}
	case *ast.IfStatement:
		if name := wxmlReturnNameFromStatement(item.Consequent); name != "" {
			return name
		}
		return wxmlReturnNameFromStatement(item.Alternate)
	case *ast.ForInStatement:
		return wxmlReturnNameFromStatement(item.Body)
	case *ast.ForOfStatement:
		return wxmlReturnNameFromStatement(item.Body)
	case *ast.ForStatement:
		return wxmlReturnNameFromStatement(item.Body)
	case *ast.WhileStatement:
		return wxmlReturnNameFromStatement(item.Body)
	case *ast.DoWhileStatement:
		return wxmlReturnNameFromStatement(item.Body)
	case *ast.LabelledStatement:
		return wxmlReturnNameFromStatement(item.Statement)
	}
	return ""
}

func renderWXMLChildren(children []*wxmlElement, indent int) string {
	var builder strings.Builder
	for _, element := range children {
		if element == nil {
			continue
		}
		if element.Text {
			builder.WriteString(strings.Repeat("  ", indent))
			builder.WriteString(element.TextValue)
			builder.WriteByte(10)
			continue
		}
		if element.Tag == "" {
			builder.WriteString(renderWXMLChildren(element.Children, indent))
			continue
		}
		padding := strings.Repeat("  ", indent)
		builder.WriteString(padding)
		builder.WriteByte(60)
		builder.WriteString(element.Tag)
		for _, attribute := range element.Attrs {
			if attribute.Name == "" {
				continue
			}
			builder.WriteByte(32)
			builder.WriteString(attribute.Name)
			if attribute.Value != nil {
				builder.WriteString("=")
				builder.WriteByte(34)
				builder.WriteString(*attribute.Value)
				builder.WriteByte(34)
			}
		}
		builder.WriteByte(62)
		if len(element.Children) == 0 {
			builder.WriteString("</")
			builder.WriteString(element.Tag)
			builder.WriteString(">")
			builder.WriteByte(10)
			continue
		}
		builder.WriteByte(10)
		builder.WriteString(renderWXMLChildren(element.Children, indent+1))
		builder.WriteString(padding)
		builder.WriteString("</")
		builder.WriteString(element.Tag)
		builder.WriteString(">")
		builder.WriteByte(10)
	}
	return builder.String()
}

func wxmlCalleeName(expression ast.Expression) string {
	switch value := expression.(type) {
	case *ast.Identifier:
		return value.Name.String()
	case *ast.DotExpression:
		left := wxmlCalleeName(value.Left)
		if left == "" {
			return value.Identifier.Name.String()
		}
		return left + "." + value.Identifier.Name.String()
	case *ast.PrivateDotExpression:
		left := wxmlCalleeName(value.Left)
		if left == "" {
			return value.Identifier.Name.String()
		}
		return left + "." + value.Identifier.Name.String()
	case *ast.BracketExpression:
		left := wxmlCalleeName(value.Left)
		member := wxmlExpressionSource(value.Member)
		if left == "" {
			return "[" + member + "]"
		}
		return left + "[" + member + "]"
	case *ast.Optional:
		return wxmlCalleeName(value.Expression)
	case *ast.OptionalChain:
		return wxmlCalleeName(value.Expression)
	default:
		return ""
	}
}

func wxmlIdentifierName(expression ast.Expression) (string, bool) {
	identifier, ok := expression.(*ast.Identifier)
	if !ok || identifier == nil || identifier.Name.String() == "" {
		return "", false
	}
	return identifier.Name.String(), true
}

func wxmlIdentifierFromExpression(expression ast.Expression) string {
	name, _ := wxmlIdentifierName(expression)
	return name
}

func wxmlStringExpression(expression ast.Expression) (string, bool) {
	value, err := evaluateStaticJavaScriptExpression(expression, map[string]staticJavaScriptValue{})
	if err != nil {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func wxmlStringArgument(call *ast.CallExpression, index int) (string, bool) {
	if call == nil || index < 0 || index >= len(call.ArgumentList) {
		return "", false
	}
	return wxmlStringExpression(call.ArgumentList[index])
}

func wxmlNumberExpression(expression ast.Expression) int {
	value, err := evaluateStaticJavaScriptExpression(expression, map[string]staticJavaScriptValue{})
	if err != nil {
		return -1
	}
	number, err := staticNumber(value)
	if err != nil || number < 0 || number != float64(int(number)) {
		return -1
	}
	return int(number)
}

func wxmlNumberArgument(call *ast.CallExpression, index int) int {
	if call == nil || index < 0 || index >= len(call.ArgumentList) {
		return -1
	}
	return wxmlNumberExpression(call.ArgumentList[index])
}

func wxmlExpressionSource(expression ast.Expression) string {
	switch value := expression.(type) {
	case *ast.Identifier:
		return value.Name.String()
	case *ast.StringLiteral:
		return value.Literal
	case *ast.NumberLiteral:
		return value.Literal
	case *ast.BooleanLiteral:
		return strconv.FormatBool(value.Value)
	case *ast.NullLiteral:
		return "null"
	case *ast.DotExpression:
		left := wxmlExpressionSource(value.Left)
		if left == "" {
			return value.Identifier.Name.String()
		}
		return left + "." + value.Identifier.Name.String()
	case *ast.PrivateDotExpression:
		left := wxmlExpressionSource(value.Left)
		if left == "" {
			return value.Identifier.Name.String()
		}
		return left + "." + value.Identifier.Name.String()
	case *ast.BracketExpression:
		return wxmlExpressionSource(value.Left) + "[" + wxmlExpressionSource(value.Member) + "]"
	case *ast.UnaryExpression:
		return value.Operator.String() + wxmlExpressionSource(value.Operand)
	case *ast.BinaryExpression:
		return wxmlExpressionSource(value.Left) + value.Operator.String() + wxmlExpressionSource(value.Right)
	default:
		return ""
	}
}

func wxmlPoolValue(zPools map[string][]string, zName string, index int) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("expression pool index is invalid")
	}
	pool, ok := zPools[zName]
	if !ok && zName == "" && len(zPools) == 1 {
		for _, candidate := range zPools {
			pool = candidate
			ok = true
		}
	}
	if !ok {
		return "", fmt.Errorf("expression pool %q is missing", zName)
	}
	if index >= len(pool) {
		return "", fmt.Errorf("expression pool index %d is out of range", index)
	}
	return pool[index], nil
}

func wxmlPathArgument(expression ast.Expression, xPool []string) (string, bool) {
	index := wxmlNumberExpression(expression)
	if index >= 0 && index < len(xPool) {
		return xPool[index], true
	}
	if value, ok := wxmlStringExpression(expression); ok {
		return value, true
	}
	return "", false
}

func stringPointer(value string) *string {
	return &value
}

func wxmlResolveElement(names map[string]*wxmlElement, fake map[string]*wxmlElement, name string) *wxmlElement {
	if name == "" {
		return nil
	}
	element := names[name]
	if override, ok := fake[name]; ok {
		if !override.Placeholder || element == nil || element == override {
			return override
		}
		element.Children = append(override.Children, element.Children...)
		element.Attrs = append(override.Attrs, element.Attrs...)
		for _, child := range override.Children {
			if child != nil {
				child.Parent = element
			}
		}
		fake[name] = element
		return element
	}
	if element != nil {
		return element
	}
	placeholder := &wxmlElement{Placeholder: true}
	names[name] = placeholder
	fake[name] = placeholder
	return placeholder
}

func wxmlPushElement(names map[string]*wxmlElement, fake map[string]*wxmlElement, parentName string, child *wxmlElement) {
	if child == nil {
		return
	}
	parent := wxmlResolveElement(names, fake, parentName)
	if parent == nil || parent == child {
		return
	}
	parent.Children = append(parent.Children, child)
	child.Parent = parent
}

func wxmlPushChild(names map[string]*wxmlElement, fake map[string]*wxmlElement, parentName, childName string) {
	child := wxmlResolveElement(names, fake, childName)
	if child == nil || child.Generator != nil {
		return
	}
	wxmlPushElement(names, fake, parentName, child)
}

func wxmlSetAttribute(element *wxmlElement, name, value string) {
	if element == nil || name == "" {
		return
	}
	for i := range element.Attrs {
		if element.Attrs[i].Name == name {
			element.Attrs[i].Value = stringPointer(value)
			return
		}
	}
	element.Attrs = append(element.Attrs, wxmlAttribute{Name: name, Value: stringPointer(value)})
}

func analyzeWXMLLoop(call *ast.CallExpression, zMode bool, zPools map[string][]string, currentZ *string, xPool []string, names map[string]*wxmlElement, fake map[string]*wxmlElement) error {
	opIndexPosition, functionPosition, parentPosition, itemPosition, indexPosition, keyPosition := 0, 1, 5, 6, 7, 8
	if zMode {
		opIndexPosition, functionPosition, parentPosition, itemPosition, indexPosition, keyPosition = 1, 2, 6, 7, 8, 9
	}
	if len(call.ArgumentList) <= keyPosition {
		return nil
	}
	parentName, ok := wxmlIdentifierName(call.ArgumentList[parentPosition])
	if !ok {
		return nil
	}
	parent := wxmlResolveElement(names, fake, parentName)
	if parent == nil {
		return nil
	}
	data, err := wxmlPoolValue(zPools, *currentZ, wxmlNumberExpression(call.ArgumentList[opIndexPosition]))
	if err != nil {
		return err
	}
	parent.Attrs = append(parent.Attrs, wxmlAttribute{Name: "wx:for", Value: stringPointer(data)})
	itemName, _ := wxmlStringExpression(call.ArgumentList[itemPosition])
	indexName, _ := wxmlStringExpression(call.ArgumentList[indexPosition])
	keyName := wxmlExpressionSource(call.ArgumentList[keyPosition])
	if itemName != "" && itemName != "item" {
		parent.Attrs = append(parent.Attrs, wxmlAttribute{Name: "wx:for-item", Value: stringPointer(itemName)})
	}
	if indexName != "" && indexName != "index" {
		parent.Attrs = append(parent.Attrs, wxmlAttribute{Name: "wx:for-index", Value: stringPointer(indexName)})
	}
	if keyName != "" && keyName != "\"\"" && keyName != "''" {
		parent.Attrs = append(parent.Attrs, wxmlAttribute{Name: "wx:key", Value: stringPointer(strings.Trim(keyName, "\"'"))})
	}
	function, ok := call.ArgumentList[functionPosition].(*ast.FunctionLiteral)
	if !ok {
		return nil
	}
	rootName := findWXMLReturnName(function)
	if rootName == "" {
		return nil
	}
	return analyzeWXMLStatements(function.Body.List, zPools, currentZ, *currentZ, xPool, names, map[string]*wxmlElement{rootName: parent})
}

func analyzeWXMLIf(statement *ast.IfStatement, zPools map[string][]string, currentZ *string, initialZ string, xPool []string, names map[string]*wxmlElement, fake map[string]*wxmlElement) error {
	condition, ok := statement.Test.(*ast.CallExpression)
	if !ok {
		return nil
	}
	value, err := wxmlConditionValue(condition, zPools, *currentZ)
	if err != nil {
		return err
	}
	target := wxmlIfTarget(statement.Consequent)
	if target == "" {
		return nil
	}
	targetElement := wxmlResolveElement(names, fake, target)
	if targetElement == nil {
		return nil
	}
	wxmlSetAttribute(targetElement, "wx:if", value)
	if body, ok := statement.Consequent.(*ast.BlockStatement); ok {
		if err := analyzeWXMLStatements(body.List, zPools, currentZ, initialZ, xPool, names, map[string]*wxmlElement{target: targetElement}); err != nil {
			return err
		}
	}
	alternate := statement.Alternate
	anchor := targetElement
	parent := targetElement.Parent
	for {
		alternateIf, ok := alternate.(*ast.IfStatement)
		if !ok {
			break
		}
		nextCondition, conditionOK := alternateIf.Test.(*ast.CallExpression)
		if !conditionOK {
			break
		}
		nextValue, nextErr := wxmlConditionValue(nextCondition, zPools, *currentZ)
		if nextErr != nil {
			return nextErr
		}
		nextBlock := &wxmlElement{Tag: "block", Attrs: []wxmlAttribute{{Name: "wx:elif", Value: stringPointer(nextValue)}}}
		if parent != nil {
			wxmlInsertAfter(parent, anchor, nextBlock)
			anchor = nextBlock
		}
		if body, bodyOK := alternateIf.Consequent.(*ast.BlockStatement); bodyOK {
			if err := analyzeWXMLStatements(body.List, zPools, currentZ, initialZ, xPool, names, map[string]*wxmlElement{target: nextBlock}); err != nil {
				return err
			}
		}
		alternate = alternateIf.Alternate
	}
	if body, ok := alternate.(*ast.BlockStatement); ok {
		elseBlock := &wxmlElement{Tag: "block", Attrs: []wxmlAttribute{{Name: "wx:else", Value: nil}}}
		if parent != nil {
			wxmlInsertAfter(parent, anchor, elseBlock)
		}
		return analyzeWXMLStatements(body.List, zPools, currentZ, initialZ, xPool, names, map[string]*wxmlElement{target: elseBlock})
	}
	return nil
}

func wxmlInsertAfter(parent, anchor, child *wxmlElement) {
	if parent == nil || child == nil {
		return
	}
	for index, existing := range parent.Children {
		if existing != anchor {
			continue
		}
		parent.Children = append(parent.Children, nil)
		copy(parent.Children[index+2:], parent.Children[index+1:])
		parent.Children[index+1] = child
		child.Parent = parent
		return
	}
	parent.Children = append(parent.Children, child)
	child.Parent = parent
}

func wxmlIfTarget(statement ast.Statement) string {
	block, ok := statement.(*ast.BlockStatement)
	if !ok || len(block.List) == 0 {
		return ""
	}
	for _, item := range block.List {
		expression, expressionOK := item.(*ast.ExpressionStatement)
		if !expressionOK {
			continue
		}
		assignment, assignmentOK := expression.Expression.(*ast.AssignExpression)
		if !assignmentOK || !tokenIsAssignment(assignment.Operator) {
			continue
		}
		if dot, dotOK := assignment.Left.(*ast.DotExpression); dotOK {
			if name, nameOK := dot.Left.(*ast.Identifier); nameOK {
				return name.Name.String()
			}
		}
		if name, nameOK := assignment.Left.(*ast.Identifier); nameOK {
			return name.Name.String()
		}
	}
	return ""
}

func wxmlConditionValue(call *ast.CallExpression, zPools map[string][]string, currentZ string) (string, error) {
	callee := wxmlCalleeName(call.Callee)
	positions := []int{0, 1, 2}
	if callee == "_oz" || callee == "_rz" {
		positions = []int{1, 0, 2}
	}
	for _, position := range positions {
		if position >= len(call.ArgumentList) {
			continue
		}
		index := wxmlNumberExpression(call.ArgumentList[position])
		if index < 0 {
			continue
		}
		if value, err := wxmlPoolValue(zPools, currentZ, index); err == nil {
			return value, nil
		}
	}
	return "", fmt.Errorf("unsupported WXML condition call %s", callee)
}

func wxmlElementFromMCall(call *ast.CallExpression, zPools map[string][]string, currentZ string, zMode bool) (*wxmlElement, error) {
	tagIndex, attrsIndex := 0, 1
	if zMode {
		tagIndex, attrsIndex = 1, 2
	}
	if len(call.ArgumentList) <= attrsIndex {
		return nil, fmt.Errorf("%s call is incomplete", wxmlCalleeName(call.Callee))
	}
	tag, ok := wxmlStringExpression(call.ArgumentList[tagIndex])
	if !ok {
		return nil, fmt.Errorf("%s tag is not static", wxmlCalleeName(call.Callee))
	}
	attributes, ok := call.ArgumentList[attrsIndex].(*ast.ArrayLiteral)
	if !ok {
		return nil, fmt.Errorf("%s attributes are not static", wxmlCalleeName(call.Callee))
	}
	element := &wxmlElement{Tag: tag}
	values := make([]staticJavaScriptValue, len(attributes.Value))
	for i, expression := range attributes.Value {
		value, err := evaluateStaticJavaScriptExpression(expression, map[string]staticJavaScriptValue{})
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	for i := 0; i+1 < len(values); i += 2 {
		name := wxmlJavaScriptValue(values[i])
		index, err := staticNumber(values[i+1])
		if err != nil {
			continue
		}
		value, valueErr := wxmlPoolValue(zPools, currentZ, int(index))
		if valueErr != nil {
			continue
		}
		element.Attrs = append(element.Attrs, wxmlAttribute{Name: name, Value: stringPointer(value)})
	}
	return element, nil
}
