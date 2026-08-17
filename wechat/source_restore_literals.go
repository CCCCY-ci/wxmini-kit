package wechat

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
)

type staticUndefined struct{}

type staticJavaScriptValue = any

func parseStaticJavaScriptExpression(source string) (staticJavaScriptValue, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("empty JavaScript expression")
	}

	program, err := parser.ParseFile(nil, "source-restore.js", "var __wxValue = ("+source+");", 0)
	if err != nil {
		return nil, err
	}
	if len(program.Body) != 1 {
		return nil, fmt.Errorf("expression contains unexpected statements")
	}
	statement, ok := program.Body[0].(*ast.VariableStatement)
	if !ok || len(statement.List) != 1 {
		return nil, fmt.Errorf("expression did not parse as a value")
	}
	return evaluateStaticJavaScriptExpression(statement.List[0].Initializer, map[string]staticJavaScriptValue{})
}

func evaluateStaticJavaScriptExpression(expression ast.Expression, variables map[string]staticJavaScriptValue) (staticJavaScriptValue, error) {
	if expression == nil {
		return staticUndefined{}, nil
	}

	switch value := expression.(type) {
	case *ast.StringLiteral:
		return value.Value.String(), nil
	case *ast.NumberLiteral:
		return staticNumberValue(value)
	case *ast.BooleanLiteral:
		return value.Value, nil
	case *ast.NullLiteral:
		return nil, nil
	case *ast.Identifier:
		name := value.Name.String()
		if result, ok := variables[name]; ok {
			return result, nil
		}
		switch name {
		case "undefined":
			return staticUndefined{}, nil
		case "NaN":
			return math.NaN(), nil
		case "Infinity":
			return math.Inf(1), nil
		default:
			return nil, fmt.Errorf("unsupported identifier %q", name)
		}
	case *ast.ArrayLiteral:
		result := make([]staticJavaScriptValue, len(value.Value))
		for i, item := range value.Value {
			parsed, err := evaluateStaticJavaScriptExpression(item, variables)
			if err != nil {
				return nil, err
			}
			result[i] = parsed
		}
		return result, nil
	case *ast.ObjectLiteral:
		result := make(map[string]staticJavaScriptValue, len(value.Value))
		for _, property := range value.Value {
			switch item := property.(type) {
			case *ast.PropertyKeyed:
				key, err := staticPropertyKey(item.Key, variables)
				if err != nil {
					return nil, err
				}
				parsed, err := evaluateStaticJavaScriptExpression(item.Value, variables)
				if err != nil {
					return nil, err
				}
				result[key] = parsed
			case *ast.PropertyShort:
				key := item.Name.Name.String()
				parsed, err := evaluateStaticJavaScriptExpression(item.Initializer, variables)
				if err != nil {
					return nil, err
				}
				result[key] = parsed
			default:
				return nil, fmt.Errorf("unsupported object property %T", property)
			}
		}
		return result, nil
	case *ast.UnaryExpression:
		operand, err := evaluateStaticJavaScriptExpression(value.Operand, variables)
		if err != nil {
			return nil, err
		}
		switch value.Operator.String() {
		case "+":
			return staticNumber(operand)
		case "-":
			number, err := staticNumber(operand)
			if err != nil {
				return nil, err
			}
			return -number, nil
		case "!":
			return !staticTruthy(operand), nil
		default:
			return nil, fmt.Errorf("unsupported unary operator %q", value.Operator.String())
		}
	case *ast.BinaryExpression:
		left, err := evaluateStaticJavaScriptExpression(value.Left, variables)
		if err != nil {
			return nil, err
		}
		right, err := evaluateStaticJavaScriptExpression(value.Right, variables)
		if err != nil {
			return nil, err
		}
		switch value.Operator.String() {
		case "+":
			if leftString, ok := left.(string); ok {
				return leftString + staticString(right), nil
			}
			if rightString, ok := right.(string); ok {
				return staticString(left) + rightString, nil
			}
			leftNumber, err := staticNumber(left)
			if err != nil {
				return nil, err
			}
			rightNumber, err := staticNumber(right)
			if err != nil {
				return nil, err
			}
			return leftNumber + rightNumber, nil
		default:
			return nil, fmt.Errorf("unsupported binary operator %q", value.Operator.String())
		}
	default:
		return nil, fmt.Errorf("unsupported static JavaScript expression %T", expression)
	}
}

func staticPropertyKey(expression ast.Expression, variables map[string]staticJavaScriptValue) (string, error) {
	value, err := evaluateStaticJavaScriptExpression(expression, variables)
	if err != nil {
		return "", err
	}
	switch key := value.(type) {
	case string:
		return key, nil
	case float64:
		return strconv.FormatFloat(key, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(key), nil
	default:
		return "", fmt.Errorf("unsupported static property key %T", value)
	}
}

func staticNumberValue(value *ast.NumberLiteral) (staticJavaScriptValue, error) {
	if value.Value != nil {
		switch number := value.Value.(type) {
		case int:
			return float64(number), nil
		case int64:
			return float64(number), nil
		case uint64:
			return float64(number), nil
		case float64:
			return number, nil
		case float32:
			return float64(number), nil
		}
	}
	parsed, err := strconv.ParseFloat(value.Literal, 64)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func staticNumber(value staticJavaScriptValue) (float64, error) {
	switch number := value.(type) {
	case int:
		return float64(number), nil
	case int64:
		return float64(number), nil
	case uint64:
		return float64(number), nil
	case float64:
		return number, nil
	case float32:
		return float64(number), nil
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func staticTruthy(value staticJavaScriptValue) bool {
	switch typed := value.(type) {
	case nil, staticUndefined:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0 && !math.IsNaN(typed)
	default:
		return true
	}
}

func staticString(value staticJavaScriptValue) string {
	switch typed := value.(type) {
	case staticUndefined:
		return "undefined"
	case nil:
		return "null"
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func staticValueToJSON(value staticJavaScriptValue) ([]byte, error) {
	return json.MarshalIndent(normalizeStaticJSONValue(value), "", "  ")
}

func normalizeStaticJSONValue(value staticJavaScriptValue) staticJavaScriptValue {
	switch typed := value.(type) {
	case staticUndefined:
		return nil
	case []staticJavaScriptValue:
		result := make([]staticJavaScriptValue, len(typed))
		for i, item := range typed {
			result[i] = normalizeStaticJSONValue(item)
		}
		return result
	case map[string]staticJavaScriptValue:
		result := make(map[string]staticJavaScriptValue, len(typed))
		for key, item := range typed {
			result[key] = normalizeStaticJSONValue(item)
		}
		return result
	default:
		return value
	}
}

func walkJavaScriptAST(node ast.Node, callback func(ast.Node)) {
	if node == nil || callback == nil {
		return
	}
	nodeType := reflect.TypeOf((*ast.Node)(nil)).Elem()
	var walkValue func(reflect.Value)
	var walkNode func(reflect.Value)
	walkNode = func(value reflect.Value) {
		if !value.IsValid() {
			return
		}
		for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			if value.Kind() == reflect.Pointer && value.Type().Implements(nodeType) && value.CanInterface() {
				callback(value.Interface().(ast.Node))
			}
			value = value.Elem()
		}
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Struct {
			for i := 0; i < value.NumField(); i++ {
				walkValue(value.Field(i))
			}
		}
	}
	walkValue = func(value reflect.Value) {
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Interface {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		if value.Kind() == reflect.Pointer {
			if value.IsNil() || !value.Type().Implements(nodeType) {
				return
			}
			walkNode(value)
			return
		}
		if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
			for i := 0; i < value.Len(); i++ {
				walkValue(value.Index(i))
			}
		}
	}
	callback(node)
	walkNode(reflect.ValueOf(node))
}

func splitJavaScriptArguments(source string) ([]string, error) {
	var arguments []string
	start := 0
	depthParen, depthBrace, depthBracket := 0, 0, 0
	for i := 0; i < len(source); {
		next, skipped := skipJavaScriptToken(source, i)
		if skipped {
			i = next
			continue
		}
		switch source[i] {
		case '(':
			depthParen++
		case ')':
			if depthParen == 0 {
				return nil, fmt.Errorf("unbalanced JavaScript argument list")
			}
			depthParen--
		case '{':
			depthBrace++
		case '}':
			if depthBrace == 0 {
				return nil, fmt.Errorf("unbalanced JavaScript argument list")
			}
			depthBrace--
		case '[':
			depthBracket++
		case ']':
			if depthBracket == 0 {
				return nil, fmt.Errorf("unbalanced JavaScript argument list")
			}
			depthBracket--
		case ',':
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				arguments = append(arguments, strings.TrimSpace(source[start:i]))
				start = i + 1
			}
		}
		i++
	}
	if depthParen != 0 || depthBrace != 0 || depthBracket != 0 {
		return nil, fmt.Errorf("unbalanced JavaScript argument list")
	}
	if strings.TrimSpace(source[start:]) != "" || len(arguments) > 0 {
		arguments = append(arguments, strings.TrimSpace(source[start:]))
	}
	return arguments, nil
}

func findJavaScriptExpressionEnd(code string, start int) int {
	depthParen, depthBrace, depthBracket := 0, 0, 0
	for i := start; i < len(code); {
		next, skipped := skipJavaScriptToken(code, i)
		if skipped {
			i = next
			continue
		}
		switch code[i] {
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '{':
			depthBrace++
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		case ';':
			if depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				return i
			}
		}
		i++
	}
	return len(code)
}

func findMatchingJavaScriptDelimiter(code string, open int, opening, closing byte) (int, error) {
	if open < 0 || open >= len(code) || code[open] != opening {
		return 0, fmt.Errorf("invalid JavaScript delimiter position")
	}
	depth := 1
	for i := open + 1; i < len(code); {
		next, skipped := skipJavaScriptToken(code, i)
		if skipped {
			i = next
			continue
		}
		if code[i] == opening {
			depth++
		} else if code[i] == closing {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("unclosed JavaScript delimiter %q", opening)
}

func tokenIsAssignment(operator token.Token) bool {
	return operator.String() == "="
}
