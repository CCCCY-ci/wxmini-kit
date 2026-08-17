package wechat

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

type glassEaselTemplateEntry struct {
	Path string
	Body string
}

type glassEaselFunction struct {
	Parameters []string
	Statements []ast.Statement
	Expression ast.Expression
}

type glassEaselEnv struct {
	Functions map[string]ast.Expression
	Values    map[string]ast.Expression
	Aliases   map[string]string
}

func newGlassEaselEnv() *glassEaselEnv {
	return &glassEaselEnv{
		Functions: make(map[string]ast.Expression),
		Values:    make(map[string]ast.Expression),
		Aliases:   make(map[string]string),
	}
}

func (env *glassEaselEnv) clone() *glassEaselEnv {
	result := newGlassEaselEnv()
	for name, value := range env.Functions {
		result.Functions[name] = value
	}
	for name, value := range env.Values {
		result.Values[name] = value
	}
	for name, value := range env.Aliases {
		result.Aliases[name] = value
	}
	return result
}

func restoreGlassEaselWXMLSources(runtimeCode []string) (map[string]string, []string) {
	result := make(map[string]string)
	var warnings []string
	for _, code := range runtimeCode {
		for _, entry := range extractGlassEaselTemplateEntries(code) {
			relativePath, err := normalizeCompiledSourcePath(entry.Path + ".wxml")
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skip glass-easel WXML path %q: %v", entry.Path, err))
				continue
			}
			root, err := parseGlassEaselRootFunction(entry.Body)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skip glass-easel WXML path %q: parse template: %v", relativePath, err))
				continue
			}
			nodes := glassRenderFunction(root, newGlassEaselEnv(), "root")
			content := renderWXMLChildren(nodes, 0)
			if strings.TrimSpace(content) == "" {
				warnings = append(warnings, fmt.Sprintf("skip glass-easel WXML path %q: renderer produced no nodes", relativePath))
				continue
			}
			result[relativePath] = content
		}
	}
	return result, warnings
}

func extractGlassEaselTemplateEntries(code string) []glassEaselTemplateEntry {
	var result []glassEaselTemplateEntry
	for offset := 0; offset < len(code); {
		index := strings.Index(code[offset:], "batchAddCompiledTemplate")
		if index < 0 {
			break
		}
		index += offset
		if !isJavaScriptIdentifierAt(code, index, "batchAddCompiledTemplate") {
			offset = index + len("batchAddCompiledTemplate")
			continue
		}
		cursor := skipJavaScriptTrivia(code, index+len("batchAddCompiledTemplate"))
		if cursor >= len(code) || code[cursor] != '(' {
			offset = index + len("batchAddCompiledTemplate")
			continue
		}
		callEnd, err := findMatchingJavaScriptDelimiter(code, cursor, '(', ')')
		if err != nil {
			break
		}
		body := code[cursor+1 : callEnd]
		returnIndex := strings.Index(body, "return {")
		if returnIndex < 0 {
			offset = callEnd + 1
			continue
		}
		objectOpen := strings.Index(body[returnIndex+len("return"):], "{")
		if objectOpen < 0 {
			offset = callEnd + 1
			continue
		}
		objectOpen += returnIndex + len("return")
		objectClose, err := findMatchingJavaScriptDelimiter(body, objectOpen, '{', '}')
		if err != nil {
			offset = callEnd + 1
			continue
		}
		for propertyOffset := objectOpen + 1; propertyOffset < objectClose; {
			if body[propertyOffset] == '\'' || body[propertyOffset] == '"' {
				key, afterKey, ok := parseJavaScriptString(body, propertyOffset)
				if !ok {
					propertyOffset++
					continue
				}
				cursor = skipJavaScriptTrivia(body, afterKey)
				if cursor >= objectClose || body[cursor] != ':' {
					propertyOffset = afterKey
					continue
				}
				cursor = skipJavaScriptTrivia(body, cursor+1)
				arrow := strings.Index(body[cursor:objectClose], "=>")
				if arrow < 0 {
					propertyOffset = afterKey
					continue
				}
				arrow += cursor
				bodyOpen := skipJavaScriptTrivia(body, arrow+2)
				if bodyOpen >= objectClose || body[bodyOpen] != '{' {
					propertyOffset = afterKey
					continue
				}
				bodyClose, bodyErr := findMatchingJavaScriptDelimiter(body, bodyOpen, '{', '}')
				if bodyErr != nil || bodyClose > objectClose {
					propertyOffset = afterKey
					continue
				}
				templateBody := body[bodyOpen+1 : bodyClose]
				if strings.Contains(templateBody, "H[\"\"") || strings.Contains(templateBody, "H['']") {
					result = append(result, glassEaselTemplateEntry{Path: key, Body: templateBody})
				}
				propertyOffset = bodyClose + 1
				continue
			}
			next, skipped := skipJavaScriptToken(body, propertyOffset)
			if skipped {
				propertyOffset = next
				continue
			}
			propertyOffset++
		}
		offset = callEnd + 1
	}
	return result
}

func parseGlassEaselRootFunction(templateBody string) (ast.Expression, error) {
	rootIndex := strings.Index(templateBody, "H[\"\"")
	if rootIndex < 0 {
		rootIndex = strings.Index(templateBody, "H['']")
	}
	if rootIndex < 0 {
		return nil, fmt.Errorf("template has no root H function")
	}
	equal := strings.IndexByte(templateBody[rootIndex:], '=')
	if equal < 0 {
		return nil, fmt.Errorf("root H function has no assignment")
	}
	equal += rootIndex
	arrowStart := skipJavaScriptTrivia(templateBody, equal+1)
	arrow := strings.Index(templateBody[arrowStart:], "=>")
	if arrow < 0 {
		return nil, fmt.Errorf("root H function is not an arrow function")
	}
	arrow += arrowStart
	bodyOpen := skipJavaScriptTrivia(templateBody, arrow+2)
	if bodyOpen >= len(templateBody) || templateBody[bodyOpen] != '{' {
		return nil, fmt.Errorf("root H function has no body")
	}
	bodyClose, err := findMatchingJavaScriptDelimiter(templateBody, bodyOpen, '{', '}')
	if err != nil {
		return nil, err
	}
	source := strings.TrimSpace(templateBody[arrowStart : bodyClose+1])
	program, err := parser.ParseFile(nil, "glass-easel-root.js", "var __glassRoot = "+source+";", 0)
	if err != nil {
		return nil, err
	}
	if len(program.Body) != 1 {
		return nil, fmt.Errorf("root function contains unexpected statements")
	}
	statement, ok := program.Body[0].(*ast.VariableStatement)
	if !ok || len(statement.List) != 1 {
		return nil, fmt.Errorf("root function did not parse as a variable initializer")
	}
	if _, err := glassFunctionFromExpression(statement.List[0].Initializer); err != nil {
		return nil, err
	}
	return statement.List[0].Initializer, nil
}

func glassFunctionFromExpression(expression ast.Expression) (*glassEaselFunction, error) {
	switch value := expression.(type) {
	case *ast.ArrowFunctionLiteral:
		parameters := glassFunctionParameters(value.ParameterList)
		if body, ok := value.Body.(*ast.BlockStatement); ok {
			return &glassEaselFunction{Parameters: parameters, Statements: body.List}, nil
		}
		if body, ok := value.Body.(*ast.ExpressionBody); ok {
			return &glassEaselFunction{Parameters: parameters, Expression: body.Expression}, nil
		}
	case *ast.FunctionLiteral:
		return &glassEaselFunction{
			Parameters: glassFunctionParameters(value.ParameterList),
			Statements: value.Body.List,
		}, nil
	}
	return nil, fmt.Errorf("expression %T is not a supported function", expression)
}

func glassFunctionParameters(parameters *ast.ParameterList) []string {
	if parameters == nil {
		return nil
	}
	result := make([]string, 0, len(parameters.List))
	for _, binding := range parameters.List {
		if binding == nil {
			continue
		}
		if identifier, ok := binding.Target.(*ast.Identifier); ok {
			result = append(result, identifier.Name.String())
		}
	}
	return result
}

func glassResolveFunction(expression ast.Expression, env *glassEaselEnv) ast.Expression {
	if identifier, ok := expression.(*ast.Identifier); ok {
		if function, exists := env.Functions[identifier.Name.String()]; exists {
			return function
		}
	}
	return expression
}

func glassRenderFunction(expression ast.Expression, parent *glassEaselEnv, role string) []*wxmlElement {
	expression = glassResolveFunction(expression, parent)
	function, err := glassFunctionFromExpression(expression)
	if err != nil {
		return nil
	}
	env := parent.clone()
	for index, parameter := range function.Parameters {
		if role == "loop" {
			switch index {
			case 0:
				env.Aliases[parameter] = "index"
			case 1:
				env.Aliases[parameter] = "item"
			}
		}
	}
	if role == "root" {
		var target ast.Expression
		for _, statement := range function.Statements {
			switch item := statement.(type) {
			case *ast.VariableStatement:
				glassBindVariableList(item.List, env)
			case *ast.LexicalDeclaration:
				glassBindVariableList(item.List, env)
			case *ast.ExpressionStatement:
				if assignment, ok := item.Expression.(*ast.AssignExpression); ok {
					glassBindAssignment(assignment, env)
				}
			case *ast.ReturnStatement:
				target = glassRootReturnFunction(item.Argument)
			}
		}
		if target != nil {
			return glassRenderFunction(target, env, "child")
		}
		return nil
	}
	if function.Expression != nil {
		return glassRenderExpression(function.Expression, env)
	}
	return glassRenderStatements(function.Statements, env)
}

func glassRootReturnFunction(expression ast.Expression) ast.Expression {
	object, ok := expression.(*ast.ObjectLiteral)
	if !ok {
		return nil
	}
	for _, property := range object.Value {
		switch item := property.(type) {
		case *ast.PropertyKeyed:
			if key, keyOK := wxmlStringExpression(item.Key); keyOK && key == "C" {
				return item.Value
			}
		case *ast.PropertyShort:
			if item.Name.Name.String() == "C" {
				return item.Initializer
			}
		}
	}
	return nil
}

func glassRenderStatements(statements []ast.Statement, env *glassEaselEnv) []*wxmlElement {
	var result []*wxmlElement
	for _, statement := range statements {
		switch item := statement.(type) {
		case *ast.VariableStatement:
			glassBindVariableList(item.List, env)
		case *ast.LexicalDeclaration:
			glassBindVariableList(item.List, env)
		case *ast.ExpressionStatement:
			result = append(result, glassRenderExpression(item.Expression, env)...)
		case *ast.IfStatement:
			result = append(result, glassRenderIf(item, env)...)
		case *ast.BlockStatement:
			result = append(result, glassRenderStatements(item.List, env)...)
		}
	}
	return result
}

func glassBindVariableList(bindings []*ast.Binding, env *glassEaselEnv) {
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		identifier, ok := binding.Target.(*ast.Identifier)
		if !ok {
			continue
		}
		name := identifier.Name.String()
		if binding.Initializer == nil {
			continue
		}
		if _, err := glassFunctionFromExpression(binding.Initializer); err == nil {
			env.Functions[name] = binding.Initializer
			continue
		}
		env.Values[name] = binding.Initializer
	}
}

func glassRenderExpression(expression ast.Expression, env *glassEaselEnv) []*wxmlElement {
	switch item := expression.(type) {
	case *ast.CallExpression:
		switch wxmlCalleeName(item.Callee) {
		case "E":
			if element := glassRenderElementCall(item, env); element != nil {
				return []*wxmlElement{element}
			}
		case "B":
			return glassRenderConditionalCall(item, env)
		case "F":
			return glassRenderLoopCall(item, env)
		case "T":
			if text, ok := glassTextFromExpression(item, env); ok {
				return []*wxmlElement{{Text: true, TextValue: text}}
			}
		}
	case *ast.ConditionalExpression:
		return append(glassRenderExpression(item.Consequent, env), glassRenderExpression(item.Alternate, env)...)
	case *ast.AssignExpression:
		glassBindAssignment(item, env)
	case *ast.SequenceExpression:
		var result []*wxmlElement
		for _, child := range item.Sequence {
			result = append(result, glassRenderExpression(child, env)...)
		}
		return result
	}
	return nil
}

func glassBindAssignment(assignment *ast.AssignExpression, env *glassEaselEnv) {
	if assignment == nil || assignment.Operator.String() != "=" {
		return
	}
	identifier, ok := assignment.Left.(*ast.Identifier)
	if !ok {
		return
	}
	if _, err := glassFunctionFromExpression(assignment.Right); err == nil {
		env.Functions[identifier.Name.String()] = assignment.Right
		return
	}
	env.Values[identifier.Name.String()] = assignment.Right
}

func glassRenderElementCall(call *ast.CallExpression, env *glassEaselEnv) *wxmlElement {
	if call == nil || len(call.ArgumentList) < 3 {
		return nil
	}
	tag, ok := wxmlStringExpression(call.ArgumentList[0])
	if !ok || tag == "" {
		return nil
	}
	element := &wxmlElement{Tag: tag}
	glassCollectAttributes(call.ArgumentList[2], env, element)
	if len(call.ArgumentList) >= 4 {
		element.Children = append(element.Children, glassRenderFunction(call.ArgumentList[3], env, "child")...)
	}
	if len(call.ArgumentList) >= 5 {
		if slot, slotOK := wxmlStringExpression(call.ArgumentList[4]); slotOK && slot != "" {
			wxmlSetAttribute(element, "slot", slot)
		}
	}
	return element
}

func glassRenderConditionalCall(call *ast.CallExpression, env *glassEaselEnv) []*wxmlElement {
	if call == nil || len(call.ArgumentList) < 2 {
		return nil
	}
	child := call.ArgumentList[len(call.ArgumentList)-1]
	children := glassRenderFunction(child, env, "child")
	if glassHasConditionalAttribute(children) {
		return children
	}
	condition := glassConditionSource(call.ArgumentList[0], env)
	return glassApplyCondition(children, condition, false)
}

func glassRenderLoopCall(call *ast.CallExpression, env *glassEaselEnv) []*wxmlElement {
	if call == nil || len(call.ArgumentList) < 5 {
		return nil
	}
	child := call.ArgumentList[len(call.ArgumentList)-1]
	children := glassRenderFunction(child, env, "loop")
	if len(children) == 0 {
		return nil
	}
	list := glassExpressionSource(call.ArgumentList[0], env)
	if list == "" {
		list = "[]"
	}
	loop := &wxmlElement{Tag: "block", Children: children}
	wxmlSetAttribute(loop, "wx:for", "{{"+list+"}}")
	if key, keyOK := wxmlStringExpression(call.ArgumentList[1]); keyOK && key != "" {
		wxmlSetAttribute(loop, "wx:key", key)
		if key == "idx" || key == "index" {
			wxmlSetAttribute(loop, "wx:for-index", key)
		}
	}
	return []*wxmlElement{loop}
}

func glassApplyCondition(children []*wxmlElement, condition string, alternate bool) []*wxmlElement {
	if len(children) == 0 {
		return nil
	}
	if condition == "" && !alternate {
		return children
	}
	if len(children) == 1 {
		if alternate {
			glassSetBooleanAttribute(children[0], "wx:else")
		} else {
			wxmlSetAttribute(children[0], "wx:if", "{{"+condition+"}}")
		}
		return children
	}
	wrapper := &wxmlElement{Tag: "block", Children: children}
	if alternate {
		glassSetBooleanAttribute(wrapper, "wx:else")
	} else {
		wxmlSetAttribute(wrapper, "wx:if", "{{"+condition+"}}")
	}
	return []*wxmlElement{wrapper}
}

func glassHasConditionalAttribute(children []*wxmlElement) bool {
	for _, child := range children {
		if child == nil {
			continue
		}
		for _, attribute := range child.Attrs {
			if attribute.Name == "wx:if" || attribute.Name == "wx:elif" || attribute.Name == "wx:else" {
				return true
			}
		}
	}
	return false
}

func glassSetBooleanAttribute(element *wxmlElement, name string) {
	if element == nil {
		return
	}
	for _, attribute := range element.Attrs {
		if attribute.Name == name {
			return
		}
	}
	element.Attrs = append(element.Attrs, wxmlAttribute{Name: name})
}

func glassRenderIf(statement *ast.IfStatement, env *glassEaselEnv) []*wxmlElement {
	if statement == nil {
		return nil
	}
	thenNodes := glassRenderStatementBranch(statement.Consequent, env)
	condition := glassConditionSource(statement.Test, env)
	result := glassApplyCondition(thenNodes, condition, false)
	if statement.Alternate != nil {
		if alternateIf, ok := statement.Alternate.(*ast.IfStatement); ok {
			result = append(result, glassRenderIf(alternateIf, env)...)
		} else {
			result = append(result, glassApplyCondition(glassRenderStatementBranch(statement.Alternate, env), "", true)...)
		}
	}
	return result
}

func glassRenderStatementBranch(statement ast.Statement, env *glassEaselEnv) []*wxmlElement {
	if block, ok := statement.(*ast.BlockStatement); ok {
		return glassRenderStatements(block.List, env)
	}
	if statement == nil {
		return nil
	}
	return glassRenderStatements([]ast.Statement{statement}, env)
}

func glassCollectAttributes(expression ast.Expression, parent *glassEaselEnv, element *wxmlElement) {
	expression = glassResolveFunction(expression, parent)
	function, err := glassFunctionFromExpression(expression)
	if err != nil {
		return
	}
	env := parent.clone()
	if function.Expression != nil {
		glassCollectAttributeExpression(function.Expression, env, element)
		return
	}
	for _, statement := range function.Statements {
		glassCollectAttributeStatement(statement, env, element)
	}
}

func glassCollectAttributeStatement(statement ast.Statement, env *glassEaselEnv, element *wxmlElement) {
	switch item := statement.(type) {
	case *ast.VariableStatement:
		glassBindVariableList(item.List, env)
	case *ast.LexicalDeclaration:
		glassBindVariableList(item.List, env)
	case *ast.ExpressionStatement:
		glassCollectAttributeExpression(item.Expression, env, element)
	case *ast.IfStatement:
		glassCollectAttributeStatement(item.Consequent, env, element)
		if item.Alternate != nil {
			glassCollectAttributeStatement(item.Alternate, env, element)
		}
	case *ast.BlockStatement:
		for _, child := range item.List {
			glassCollectAttributeStatement(child, env, element)
		}
	}
}

func glassCollectAttributeExpression(expression ast.Expression, env *glassEaselEnv, element *wxmlElement) {
	switch item := expression.(type) {
	case *ast.CallExpression:
		name := wxmlCalleeName(item.Callee)
		switch name {
		case "L":
			if len(item.ArgumentList) >= 2 {
				if value, ok := glassAttributeValue(item.ArgumentList[1], env); ok {
					wxmlSetAttribute(element, "class", value)
				}
			}
		case "O":
			if len(item.ArgumentList) >= 3 {
				name, nameOK := wxmlStringExpression(item.ArgumentList[1])
				value, valueOK := glassAttributeValue(item.ArgumentList[2], env)
				if nameOK && valueOK {
					wxmlSetAttribute(element, name, value)
				}
			}
		case "R.d":
			if len(item.ArgumentList) >= 3 {
				name, nameOK := wxmlStringExpression(item.ArgumentList[1])
				value, valueOK := glassAttributeValue(item.ArgumentList[2], env)
				if nameOK && valueOK {
					wxmlSetAttribute(element, "data-"+name, value)
				}
			}
		case "R.v":
			if len(item.ArgumentList) >= 3 {
				event, eventOK := wxmlStringExpression(item.ArgumentList[1])
				handler, handlerOK := wxmlStringExpression(item.ArgumentList[2])
				if eventOK && handlerOK {
					wxmlSetAttribute(element, "bind:"+event, handler)
				}
			}
		case "R.y":
			if len(item.ArgumentList) >= 2 {
				if value, ok := glassAttributeValue(item.ArgumentList[1], env); ok {
					wxmlSetAttribute(element, "style", value)
				}
			}
		}
	case *ast.AssignExpression:
		glassBindAssignment(item, env)
	case *ast.ConditionalExpression:
		glassCollectAttributeExpression(item.Consequent, env, element)
		glassCollectAttributeExpression(item.Alternate, env, element)
	}
}

func glassTextFromExpression(call *ast.CallExpression, env *glassEaselEnv) (string, bool) {
	if call == nil || len(call.ArgumentList) == 0 {
		return "", false
	}
	expression := call.ArgumentList[0]
	if value, ok := glassUnwrappedString(expression, env); ok {
		return value, true
	}
	source := glassExpressionSource(expression, env)
	if source == "" {
		return "", false
	}
	return "{{" + source + "}}", true
}

func glassUnwrappedString(expression ast.Expression, env *glassEaselEnv) (string, bool) {
	if call, ok := expression.(*ast.CallExpression); ok {
		name := wxmlCalleeName(call.Callee)
		if (name == "X" || name == "Y") && len(call.ArgumentList) > 0 {
			return glassUnwrappedString(call.ArgumentList[0], env)
		}
	}
	return wxmlStringExpression(expression)
}

func glassAttributeValue(expression ast.Expression, env *glassEaselEnv) (string, bool) {
	if value, ok := glassUnwrappedString(expression, env); ok {
		return value, true
	}
	if number := wxmlNumberExpression(expression); number >= 0 {
		return "{{" + strconv.Itoa(number) + "}}", true
	}
	if boolean, ok := expression.(*ast.BooleanLiteral); ok {
		return "{{" + strconv.FormatBool(boolean.Value) + "}}", true
	}
	source := glassExpressionSource(expression, env)
	if source == "" {
		return "", false
	}
	return "{{" + source + "}}", true
}

func glassConditionSource(expression ast.Expression, env *glassEaselEnv) string {
	if expression == nil {
		return ""
	}
	if binary, ok := expression.(*ast.BinaryExpression); ok {
		operator := binary.Operator.String()
		if operator == "===" || operator == "==" || operator == "!==" || operator == "!=" {
			if identifier, identifierOK := binary.Left.(*ast.Identifier); identifierOK {
				if assigned, assignedOK := env.Values[identifier.Name.String()]; assignedOK {
					if base, baseOK := glassAssignedCondition(assigned, env); baseOK {
						if number, numberOK := glassStaticInteger(binary.Right); numberOK {
							if (number == 1 && operator != "!==" && operator != "!=") ||
								(number == 0 && (operator == "!==" || operator == "!=")) {
								return base
							}
							if (number == 0 && operator != "!==" && operator != "!=") ||
								(number == 1 && (operator == "!==" || operator == "!=")) {
								return "!(" + base + ")"
							}
						}
					}
				}
			}
		}
	}
	if identifier, ok := expression.(*ast.Identifier); ok {
		if assigned, exists := env.Values[identifier.Name.String()]; exists {
			if condition, conditionOK := glassAssignedCondition(assigned, env); conditionOK {
				return condition
			}
		}
	}
	source := glassExpressionSource(expression, env)
	if glassCompilerGuard(source) {
		return ""
	}
	return source
}

func glassAssignedCondition(expression ast.Expression, env *glassEaselEnv) (string, bool) {
	conditional, ok := expression.(*ast.ConditionalExpression)
	if !ok {
		return "", false
	}
	consequent, consequentOK := glassStaticInteger(conditional.Consequent)
	alternate, alternateOK := glassStaticInteger(conditional.Alternate)
	if !consequentOK || !alternateOK {
		return "", false
	}
	source := glassExpressionSource(conditional.Test, env)
	if source == "" {
		return "", false
	}
	if consequent == 1 && alternate == 0 {
		return source, true
	}
	if consequent == 0 && alternate == 1 {
		return "!(" + source + ")", true
	}
	return "", false
}

func glassStaticInteger(expression ast.Expression) (int, bool) {
	value, err := evaluateStaticJavaScriptExpression(expression, map[string]staticJavaScriptValue{})
	if err != nil {
		return 0, false
	}
	number, err := staticNumber(value)
	if err != nil || number != float64(int(number)) {
		return 0, false
	}
	return int(number), true
}

func glassCompilerGuard(source string) bool {
	source = strings.TrimSpace(source)
	if source == "" || source == "C" || source == "K" || source == "undefined" {
		return true
	}
	return strings.HasPrefix(source, "C || K") || strings.HasPrefix(source, "C||K")
}

func glassExpressionSource(expression ast.Expression, env *glassEaselEnv) string {
	return glassExpressionSourceSeen(expression, env, make(map[string]bool))
}

func glassExpressionSourceSeen(expression ast.Expression, env *glassEaselEnv, seen map[string]bool) string {
	if expression == nil {
		return ""
	}
	switch value := expression.(type) {
	case *ast.Identifier:
		name := value.Name.String()
		if alias, exists := env.Aliases[name]; exists {
			return alias
		}
		if assigned, exists := env.Values[name]; exists && !seen[name] {
			seen[name] = true
			result := glassExpressionSourceSeen(assigned, env, seen)
			delete(seen, name)
			if result != "" {
				return result
			}
		}
		if name == "D" || name == "U" {
			return ""
		}
		return name
	case *ast.StringLiteral:
		return glassJavaScriptStringLiteral(value.Value.String())
	case *ast.NumberLiteral:
		return value.Literal
	case *ast.BooleanLiteral:
		return strconv.FormatBool(value.Value)
	case *ast.NullLiteral:
		return "null"
	case *ast.DotExpression:
		if left, ok := value.Left.(*ast.Identifier); ok && (left.Name.String() == "D" || left.Name.String() == "U") {
			return value.Identifier.Name.String()
		}
		left := glassExpressionSourceSeen(value.Left, env, seen)
		if left == "" {
			return value.Identifier.Name.String()
		}
		return left + "." + value.Identifier.Name.String()
	case *ast.PrivateDotExpression:
		left := glassExpressionSourceSeen(value.Left, env, seen)
		if left == "" {
			return value.Identifier.Name.String()
		}
		return left + "." + value.Identifier.Name.String()
	case *ast.BracketExpression:
		left := glassExpressionSourceSeen(value.Left, env, seen)
		right := glassExpressionSourceSeen(value.Member, env, seen)
		return left + "[" + right + "]"
	case *ast.CallExpression:
		name := wxmlCalleeName(value.Callee)
		if (name == "X" || name == "Y") && len(value.ArgumentList) > 0 {
			return glassExpressionSourceSeen(value.ArgumentList[0], env, seen)
		}
		var arguments []string
		for _, argument := range value.ArgumentList {
			arguments = append(arguments, glassExpressionSourceSeen(argument, env, seen))
		}
		return name + "(" + strings.Join(arguments, ",") + ")"
	case *ast.UnaryExpression:
		return value.Operator.String() + glassExpressionSourceSeen(value.Operand, env, seen)
	case *ast.BinaryExpression:
		left := glassExpressionSourceSeen(value.Left, env, seen)
		right := glassExpressionSourceSeen(value.Right, env, seen)
		return left + " " + value.Operator.String() + " " + right
	case *ast.ConditionalExpression:
		return "(" + glassExpressionSourceSeen(value.Test, env, seen) + " ? " +
			glassExpressionSourceSeen(value.Consequent, env, seen) + " : " +
			glassExpressionSourceSeen(value.Alternate, env, seen) + ")"
	case *ast.SequenceExpression:
		var values []string
		for _, item := range value.Sequence {
			values = append(values, glassExpressionSourceSeen(item, env, seen))
		}
		return strings.Join(values, ",")
	case *ast.ArrayLiteral:
		var values []string
		for _, item := range value.Value {
			values = append(values, glassExpressionSourceSeen(item, env, seen))
		}
		return "[" + strings.Join(values, ",") + "]"
	default:
		return wxmlExpressionSource(expression)
	}
}

func glassJavaScriptStringLiteral(value string) string {
	value = strings.ReplaceAll(value, string(rune(92)), string(rune(92))+string(rune(92)))
	value = strings.ReplaceAll(value, "'", string(rune(92))+"'")
	return "'" + value + "'"
}
