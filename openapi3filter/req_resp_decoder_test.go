package openapi3filter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jeffy-mathew/kin-openapi/openapi3"
	legacyrouter "github.com/jeffy-mathew/kin-openapi/routers/legacy"
	"github.com/stretchr/testify/require"
)

func TestDecodeParameter(t *testing.T) {
	var (
		boolPtr   = func(b bool) *bool { return &b }
		explode   = boolPtr(true)
		noExplode = boolPtr(false)
		arrayOf   = func(items *openapi3.SchemaRef) *openapi3.SchemaRef {
			return &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "array", Items: items}}
		}
		objectOf = func(args ...interface{}) *openapi3.SchemaRef {
			s := &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "object", Properties: make(map[string]*openapi3.SchemaRef)}}
			if len(args)%2 != 0 {
				panic("invalid arguments. must be an odd number of arguments")
			}
			for i := 0; i < len(args)/2; i++ {
				propName := args[i*2].(string)
				propSchema := args[i*2+1].(*openapi3.SchemaRef)
				s.Value.Properties[propName] = propSchema
			}
			return s
		}

		integerSchema = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "integer"}}
		numberSchema  = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "number"}}
		booleanSchema = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "boolean"}}
		stringSchema  = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "string"}}
		allofSchema   = &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				AllOf: []*openapi3.SchemaRef{
					integerSchema,
					numberSchema,
				}}}
		anyofSchema = &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				AnyOf: []*openapi3.SchemaRef{
					integerSchema,
					stringSchema,
				}}}
		oneofSchema = &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				OneOf: []*openapi3.SchemaRef{
					booleanSchema,
					integerSchema,
				}}}
		arraySchema  = arrayOf(stringSchema)
		objectSchema = objectOf("id", stringSchema, "name", stringSchema)
	)

	type testCase struct {
		name   string
		param  *openapi3.Parameter
		path   string
		query  string
		header string
		cookie string
		want   interface{}
		err    error
	}

	testGroups := []struct {
		name      string
		testCases []testCase
	}{
		{
			name: "path primitive",
			testCases: []testCase{
				{
					name:  "simple",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "simple", Explode: noExplode, Schema: stringSchema},
					path:  "/foo",
					want:  "foo",
				},
				{
					name:  "simple explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "simple", Explode: explode, Schema: stringSchema},
					path:  "/foo",
					want:  "foo",
				},
				{
					name:  "label",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: noExplode, Schema: stringSchema},
					path:  "/.foo",
					want:  "foo",
				},
				{
					name:  "label invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: noExplode, Schema: stringSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "label explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: explode, Schema: stringSchema},
					path:  "/.foo",
					want:  "foo",
				},
				{
					name:  "label explode invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: explode, Schema: stringSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "matrix",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: noExplode, Schema: stringSchema},
					path:  "/;param=foo",
					want:  "foo",
				},
				{
					name:  "matrix invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: noExplode, Schema: stringSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "matrix explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: explode, Schema: stringSchema},
					path:  "/;param=foo",
					want:  "foo",
				},
				{
					name:  "matrix explode invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: explode, Schema: stringSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "default",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: stringSchema},
					path:  "/foo",
					want:  "foo",
				},
				{
					name:  "string",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: stringSchema},
					path:  "/foo",
					want:  "foo",
				},
				{
					name:  "integer",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: integerSchema},
					path:  "/1",
					want:  float64(1),
				},
				{
					name:  "integer invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: integerSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "number",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: numberSchema},
					path:  "/1.1",
					want:  1.1,
				},
				{
					name:  "number invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: numberSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "boolean",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: booleanSchema},
					path:  "/true",
					want:  true,
				},
				{
					name:  "boolean invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: booleanSchema},
					path:  "/foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
			},
		},
		{
			name: "path array",
			testCases: []testCase{
				{
					name:  "simple",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "simple", Explode: noExplode, Schema: arraySchema},
					path:  "/foo,bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "simple explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "simple", Explode: explode, Schema: arraySchema},
					path:  "/foo,bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "label",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: noExplode, Schema: arraySchema},
					path:  "/.foo,bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "label invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: noExplode, Schema: arraySchema},
					path:  "/foo,bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo,bar"},
				},
				{
					name:  "label explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: explode, Schema: arraySchema},
					path:  "/.foo.bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "label explode invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: explode, Schema: arraySchema},
					path:  "/foo.bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo.bar"},
				},
				{
					name:  "matrix",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: noExplode, Schema: arraySchema},
					path:  "/;param=foo,bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "matrix invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: noExplode, Schema: arraySchema},
					path:  "/foo,bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo,bar"},
				},
				{
					name:  "matrix explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: explode, Schema: arraySchema},
					path:  "/;param=foo;param=bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "matrix explode invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: explode, Schema: arraySchema},
					path:  "/foo,bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo,bar"},
				},
				{
					name:  "default",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: arraySchema},
					path:  "/foo,bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "invalid integer items",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: arrayOf(integerSchema)},
					path:  "/1,foo",
					err:   &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:  "invalid number items",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: arrayOf(numberSchema)},
					path:  "/1.1,foo",
					err:   &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:  "invalid boolean items",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: arrayOf(booleanSchema)},
					path:  "/true,foo",
					err:   &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
			},
		},
		{
			name: "path object",
			testCases: []testCase{
				{
					name:  "simple",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "simple", Explode: noExplode, Schema: objectSchema},
					path:  "/id,foo,name,bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "simple explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "simple", Explode: explode, Schema: objectSchema},
					path:  "/id=foo,name=bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "label",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: noExplode, Schema: objectSchema},
					path:  "/.id,foo,name,bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "label invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: noExplode, Schema: objectSchema},
					path:  "/id,foo,name,bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "id,foo,name,bar"},
				},
				{
					name:  "label explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: explode, Schema: objectSchema},
					path:  "/.id=foo.name=bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "label explode invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "label", Explode: explode, Schema: objectSchema},
					path:  "/id=foo.name=bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "id=foo.name=bar"},
				},
				{
					name:  "matrix",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: noExplode, Schema: objectSchema},
					path:  "/;param=id,foo,name,bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "matrix invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: noExplode, Schema: objectSchema},
					path:  "/id,foo,name,bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "id,foo,name,bar"},
				},
				{
					name:  "matrix explode",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: explode, Schema: objectSchema},
					path:  "/;id=foo;name=bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "matrix explode invalid",
					param: &openapi3.Parameter{Name: "param", In: "path", Style: "matrix", Explode: explode, Schema: objectSchema},
					path:  "/id=foo;name=bar",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "id=foo;name=bar"},
				},
				{
					name:  "default",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: objectSchema},
					path:  "/id,foo,name,bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "invalid integer prop",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: objectOf("foo", integerSchema)},
					path:  "/foo,bar",
					err:   &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:  "invalid number prop",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: objectOf("foo", numberSchema)},
					path:  "/foo,bar",
					err:   &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:  "invalid boolean prop",
					param: &openapi3.Parameter{Name: "param", In: "path", Schema: objectOf("foo", booleanSchema)},
					path:  "/foo,bar",
					err:   &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
			},
		},
		{
			name: "query primitive",
			testCases: []testCase{
				{
					name:  "form",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "form", Explode: noExplode, Schema: stringSchema},
					query: "param=foo",
					want:  "foo",
				},
				{
					name:  "form explode",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "form", Explode: explode, Schema: stringSchema},
					query: "param=foo",
					want:  "foo",
				},
				{
					name:  "default",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: stringSchema},
					query: "param=foo",
					want:  "foo",
				},
				{
					name:  "string",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: stringSchema},
					query: "param=foo",
					want:  "foo",
				},
				{
					name:  "integer",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: integerSchema},
					query: "param=1",
					want:  float64(1),
				},
				{
					name:  "integer invalid",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: integerSchema},
					query: "param=foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "number",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: numberSchema},
					query: "param=1.1",
					want:  1.1,
				},
				{
					name:  "number invalid",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: numberSchema},
					query: "param=foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:  "boolean",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: booleanSchema},
					query: "param=true",
					want:  true,
				},
				{
					name:  "boolean invalid",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: booleanSchema},
					query: "param=foo",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
			},
		},
		{
			name: "query Allof",
			testCases: []testCase{
				{
					name:  "allofSchema integer and number",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: allofSchema},
					query: "param=1",
					want:  float64(1),
				},
				{
					name:  "allofSchema string",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: allofSchema},
					query: "param=abdf",
					err:   &ParseError{Kind: KindInvalidFormat, Value: "abdf"},
				},
			},
		},
		{
			name: "query Anyof",
			testCases: []testCase{
				{
					name:  "anyofSchema integer",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: anyofSchema},
					query: "param=1",
					want:  float64(1),
				},
				{
					name:  "anyofSchema string",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: anyofSchema},
					query: "param=abdf",
					want:  "abdf",
				},
			},
		},
		{
			name: "query Oneof",
			testCases: []testCase{
				{
					name:  "oneofSchema boolean",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: oneofSchema},
					query: "param=true",
					want:  true,
				},
				{
					name:  "oneofSchema int",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: oneofSchema},
					query: "param=1122",
					want:  float64(1122),
				},
				{
					name:  "oneofSchema string",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: oneofSchema},
					query: "param=abcd",
					want:  nil,
				},
			},
		},
		{
			name: "query array",
			testCases: []testCase{
				{
					name:  "form",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "form", Explode: noExplode, Schema: arraySchema},
					query: "param=foo,bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "form explode",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "form", Explode: explode, Schema: arraySchema},
					query: "param=foo&param=bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "spaceDelimited",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "spaceDelimited", Explode: noExplode, Schema: arraySchema},
					query: "param=foo bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "spaceDelimited explode",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "spaceDelimited", Explode: explode, Schema: arraySchema},
					query: "param=foo&param=bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "pipeDelimited",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "pipeDelimited", Explode: noExplode, Schema: arraySchema},
					query: "param=foo|bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "pipeDelimited explode",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "pipeDelimited", Explode: explode, Schema: arraySchema},
					query: "param=foo&param=bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "default",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: arraySchema},
					query: "param=foo&param=bar",
					want:  []interface{}{"foo", "bar"},
				},
				{
					name:  "invalid integer items",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: arrayOf(integerSchema)},
					query: "param=1&param=foo",
					err:   &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:  "invalid number items",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: arrayOf(numberSchema)},
					query: "param=1.1&param=foo",
					err:   &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:  "invalid boolean items",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: arrayOf(booleanSchema)},
					query: "param=true&param=foo",
					err:   &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
			},
		},
		{
			name: "query object",
			testCases: []testCase{
				{
					name:  "form",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "form", Explode: noExplode, Schema: objectSchema},
					query: "param=id,foo,name,bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "form explode",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "form", Explode: explode, Schema: objectSchema},
					query: "id=foo&name=bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "deepObject explode",
					param: &openapi3.Parameter{Name: "param", In: "query", Style: "deepObject", Explode: explode, Schema: objectSchema},
					query: "param[id]=foo&param[name]=bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "default",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: objectSchema},
					query: "id=foo&name=bar",
					want:  map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:  "invalid integer prop",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: objectOf("foo", integerSchema)},
					query: "foo=bar",
					err:   &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:  "invalid number prop",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: objectOf("foo", numberSchema)},
					query: "foo=bar",
					err:   &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:  "invalid boolean prop",
					param: &openapi3.Parameter{Name: "param", In: "query", Schema: objectOf("foo", booleanSchema)},
					query: "foo=bar",
					err:   &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
			},
		},
		{
			name: "header primitive",
			testCases: []testCase{
				{
					name:   "simple",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Style: "simple", Explode: noExplode, Schema: stringSchema},
					header: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "simple explode",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Style: "simple", Explode: explode, Schema: stringSchema},
					header: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "default",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: stringSchema},
					header: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "string",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: stringSchema},
					header: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "integer",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: integerSchema},
					header: "X-Param:1",
					want:   float64(1),
				},
				{
					name:   "integer invalid",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: integerSchema},
					header: "X-Param:foo",
					err:    &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:   "number",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: numberSchema},
					header: "X-Param:1.1",
					want:   1.1,
				},
				{
					name:   "number invalid",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: numberSchema},
					header: "X-Param:foo",
					err:    &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:   "boolean",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: booleanSchema},
					header: "X-Param:true",
					want:   true,
				},
				{
					name:   "boolean invalid",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: booleanSchema},
					header: "X-Param:foo",
					err:    &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
			},
		},
		{
			name: "header array",
			testCases: []testCase{
				{
					name:   "simple",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Style: "simple", Explode: noExplode, Schema: arraySchema},
					header: "X-Param:foo,bar",
					want:   []interface{}{"foo", "bar"},
				},
				{
					name:   "simple explode",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Style: "simple", Explode: explode, Schema: arraySchema},
					header: "X-Param:foo,bar",
					want:   []interface{}{"foo", "bar"},
				},
				{
					name:   "default",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: arraySchema},
					header: "X-Param:foo,bar",
					want:   []interface{}{"foo", "bar"},
				},
				{
					name:   "invalid integer items",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: arrayOf(integerSchema)},
					header: "X-Param:1,foo",
					err:    &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:   "invalid number items",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: arrayOf(numberSchema)},
					header: "X-Param:1.1,foo",
					err:    &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:   "invalid boolean items",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: arrayOf(booleanSchema)},
					header: "X-Param:true,foo",
					err:    &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
			},
		},
		{
			name: "header object",
			testCases: []testCase{
				{
					name:   "simple",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Style: "simple", Explode: noExplode, Schema: objectSchema},
					header: "X-Param:id,foo,name,bar",
					want:   map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:   "simple explode",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Style: "simple", Explode: explode, Schema: objectSchema},
					header: "X-Param:id=foo,name=bar",
					want:   map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:   "default",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: objectSchema},
					header: "X-Param:id,foo,name,bar",
					want:   map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:   "invalid integer prop",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: objectOf("foo", integerSchema)},
					header: "X-Param:foo,bar",
					err:    &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:   "invalid number prop",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: objectOf("foo", numberSchema)},
					header: "X-Param:foo,bar",
					err:    &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:   "invalid boolean prop",
					param:  &openapi3.Parameter{Name: "X-Param", In: "header", Schema: objectOf("foo", booleanSchema)},
					header: "X-Param:foo,bar",
					err:    &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
			},
		},
		{
			name: "cookie primitive",
			testCases: []testCase{
				{
					name:   "form",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: stringSchema},
					cookie: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "form explode",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: explode, Schema: stringSchema},
					cookie: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "default",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: stringSchema},
					cookie: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "string",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: stringSchema},
					cookie: "X-Param:foo",
					want:   "foo",
				},
				{
					name:   "integer",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: integerSchema},
					cookie: "X-Param:1",
					want:   float64(1),
				},
				{
					name:   "integer invalid",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: integerSchema},
					cookie: "X-Param:foo",
					err:    &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:   "number",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: numberSchema},
					cookie: "X-Param:1.1",
					want:   1.1,
				},
				{
					name:   "number invalid",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: numberSchema},
					cookie: "X-Param:foo",
					err:    &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
				{
					name:   "boolean",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: booleanSchema},
					cookie: "X-Param:true",
					want:   true,
				},
				{
					name:   "boolean invalid",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Schema: booleanSchema},
					cookie: "X-Param:foo",
					err:    &ParseError{Kind: KindInvalidFormat, Value: "foo"},
				},
			},
		},
		{
			name: "cookie array",
			testCases: []testCase{
				{
					name:   "form",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: arraySchema},
					cookie: "X-Param:foo,bar",
					want:   []interface{}{"foo", "bar"},
				},
				{
					name:   "invalid integer items",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: arrayOf(integerSchema)},
					cookie: "X-Param:1,foo",
					err:    &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:   "invalid number items",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: arrayOf(numberSchema)},
					cookie: "X-Param:1.1,foo",
					err:    &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
				{
					name:   "invalid boolean items",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: arrayOf(booleanSchema)},
					cookie: "X-Param:true,foo",
					err:    &ParseError{path: []interface{}{1}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "foo"}},
				},
			},
		},
		{
			name: "cookie object",
			testCases: []testCase{
				{
					name:   "form",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: objectSchema},
					cookie: "X-Param:id,foo,name,bar",
					want:   map[string]interface{}{"id": "foo", "name": "bar"},
				},
				{
					name:   "invalid integer prop",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: objectOf("foo", integerSchema)},
					cookie: "X-Param:foo,bar",
					err:    &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:   "invalid number prop",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: objectOf("foo", numberSchema)},
					cookie: "X-Param:foo,bar",
					err:    &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
				{
					name:   "invalid boolean prop",
					param:  &openapi3.Parameter{Name: "X-Param", In: "cookie", Style: "form", Explode: noExplode, Schema: objectOf("foo", booleanSchema)},
					cookie: "X-Param:foo,bar",
					err:    &ParseError{path: []interface{}{"foo"}, Cause: &ParseError{Kind: KindInvalidFormat, Value: "bar"}},
				},
			},
		},
	}

	for _, tg := range testGroups {
		t.Run(tg.name, func(t *testing.T) {
			for _, tc := range tg.testCases {
				t.Run(tc.name, func(t *testing.T) {
					req, err := http.NewRequest(http.MethodGet, "http://test.org/test"+tc.path, nil)
					require.NoError(t, err, "failed to create a test request")

					if tc.query != "" {
						query := req.URL.Query()
						for _, param := range strings.Split(tc.query, "&") {
							v := strings.Split(param, "=")
							query.Add(v[0], v[1])
						}
						req.URL.RawQuery = query.Encode()
					}

					if tc.header != "" {
						v := strings.Split(tc.header, ":")
						req.Header.Add(v[0], v[1])
					}

					if tc.cookie != "" {
						v := strings.Split(tc.cookie, ":")
						req.AddCookie(&http.Cookie{Name: v[0], Value: v[1]})
					}

					path := "/test"
					if tc.path != "" {
						path += "/{" + tc.param.Name + "}"
					}

					info := &openapi3.Info{
						Title:   "MyAPI",
						Version: "0.1",
					}
					spec := &openapi3.T{OpenAPI: "3.0.0", Info: info}
					op := &openapi3.Operation{
						OperationID: "test",
						Parameters:  []*openapi3.ParameterRef{{Value: tc.param}},
						Responses:   openapi3.NewResponses(),
					}
					spec.AddOperation(path, http.MethodGet, op)
					err = spec.Validate(context.Background())
					require.NoError(t, err)
					router, err := legacyrouter.NewRouter(spec)
					require.NoError(t, err)

					route, pathParams, err := router.FindRoute(req)
					require.NoError(t, err)

					input := &RequestValidationInput{Request: req, PathParams: pathParams, Route: route}
					got, err := decodeStyledParameter(tc.param, input)

					if tc.err != nil {
						require.Error(t, err)
						require.Truef(t, matchParseError(err, tc.err), "got error:\n%v\nwant error:\n%v", err, tc.err)
						return
					}

					require.NoError(t, err)
					require.Truef(t, reflect.DeepEqual(got, tc.want), "got %v, want %v", got, tc.want)
				})
			}
		})
	}
}

func TestDecodeBody(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	urlencodedForm := make(url.Values)
	urlencodedForm.Set("a", "a1")
	urlencodedForm.Set("b", "10")
	urlencodedForm.Add("c", "c1")
	urlencodedForm.Add("c", "c2")

	urlencodedSpaceDelim := make(url.Values)
	urlencodedSpaceDelim.Set("a", "a1")
	urlencodedSpaceDelim.Set("b", "10")
	urlencodedSpaceDelim.Add("c", "c1 c2")

	urlencodedPipeDelim := make(url.Values)
	urlencodedPipeDelim.Set("a", "a1")
	urlencodedPipeDelim.Set("b", "10")
	urlencodedPipeDelim.Add("c", "c1|c2")

	d, err := json.Marshal(map[string]interface{}{"d1": "d1"})
	require.NoError(t, err)
	multipartForm, multipartFormMime, err := newTestMultipartForm([]*testFormPart{
		{name: "a", contentType: "text/plain", data: strings.NewReader("a1")},
		{name: "b", contentType: "application/json", data: strings.NewReader("10")},
		{name: "c", contentType: "text/plain", data: strings.NewReader("c1")},
		{name: "c", contentType: "text/plain", data: strings.NewReader("c2")},
		{name: "d", contentType: "application/json", data: bytes.NewReader(d)},
		{name: "f", contentType: "application/octet-stream", data: strings.NewReader("foo"), filename: "f1"},
		{name: "g", data: strings.NewReader("g1")},
	})
	require.NoError(t, err)

	multipartFormExtraPart, multipartFormMimeExtraPart, err := newTestMultipartForm([]*testFormPart{
		{name: "a", contentType: "text/plain", data: strings.NewReader("a1")},
		{name: "x", contentType: "text/plain", data: strings.NewReader("x1")},
	})
	require.NoError(t, err)

	multipartAnyAdditionalProps, multipartMimeAnyAdditionalProps, err := newTestMultipartForm([]*testFormPart{
		{name: "a", contentType: "text/plain", data: strings.NewReader("a1")},
		{name: "x", contentType: "text/plain", data: strings.NewReader("x1")},
	})
	require.NoError(t, err)

	multipartAdditionalProps, multipartMimeAdditionalProps, err := newTestMultipartForm([]*testFormPart{
		{name: "a", contentType: "text/plain", data: strings.NewReader("a1")},
		{name: "x", contentType: "text/plain", data: strings.NewReader("x1")},
	})
	require.NoError(t, err)

	multipartAdditionalPropsErr, multipartMimeAdditionalPropsErr, err := newTestMultipartForm([]*testFormPart{
		{name: "a", contentType: "text/plain", data: strings.NewReader("a1")},
		{name: "x", contentType: "text/plain", data: strings.NewReader("x1")},
		{name: "y", contentType: "text/plain", data: strings.NewReader("y1")},
	})
	require.NoError(t, err)

	testCases := []struct {
		name     string
		mime     string
		body     io.Reader
		schema   *openapi3.Schema
		encoding map[string]*openapi3.Encoding
		want     interface{}
		wantErr  error
	}{
		{
			name:    prefixUnsupportedCT,
			mime:    "application/xml",
			wantErr: &ParseError{Kind: KindUnsupportedFormat},
		},
		{
			name:    "invalid body data",
			mime:    "application/json",
			body:    strings.NewReader("invalid"),
			wantErr: &ParseError{Kind: KindInvalidFormat},
		},
		{
			name: "plain text",
			mime: "text/plain",
			body: strings.NewReader("text"),
			want: "text",
		},
		{
			name: "json",
			mime: "application/json",
			body: strings.NewReader("\"foo\""),
			want: "foo",
		},
		{
			name: "x-yaml",
			mime: "application/x-yaml",
			body: strings.NewReader("foo"),
			want: "foo",
		},
		{
			name: "yaml",
			mime: "application/yaml",
			body: strings.NewReader("foo"),
			want: "foo",
		},
		{
			name: "urlencoded form",
			mime: "application/x-www-form-urlencoded",
			body: strings.NewReader(urlencodedForm.Encode()),
			schema: openapi3.NewObjectSchema().
				WithProperty("a", openapi3.NewStringSchema()).
				WithProperty("b", openapi3.NewIntegerSchema()).
				WithProperty("c", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())),
			want: map[string]interface{}{"a": "a1", "b": float64(10), "c": []interface{}{"c1", "c2"}},
		},
		{
			name: "urlencoded space delimited",
			mime: "application/x-www-form-urlencoded",
			body: strings.NewReader(urlencodedSpaceDelim.Encode()),
			schema: openapi3.NewObjectSchema().
				WithProperty("a", openapi3.NewStringSchema()).
				WithProperty("b", openapi3.NewIntegerSchema()).
				WithProperty("c", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())),
			encoding: map[string]*openapi3.Encoding{
				"c": {Style: openapi3.SerializationSpaceDelimited, Explode: boolPtr(false)},
			},
			want: map[string]interface{}{"a": "a1", "b": float64(10), "c": []interface{}{"c1", "c2"}},
		},
		{
			name: "urlencoded pipe delimited",
			mime: "application/x-www-form-urlencoded",
			body: strings.NewReader(urlencodedPipeDelim.Encode()),
			schema: openapi3.NewObjectSchema().
				WithProperty("a", openapi3.NewStringSchema()).
				WithProperty("b", openapi3.NewIntegerSchema()).
				WithProperty("c", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())),
			encoding: map[string]*openapi3.Encoding{
				"c": {Style: openapi3.SerializationPipeDelimited, Explode: boolPtr(false)},
			},
			want: map[string]interface{}{"a": "a1", "b": float64(10), "c": []interface{}{"c1", "c2"}},
		},
		{
			name: "multipart",
			mime: multipartFormMime,
			body: multipartForm,
			schema: openapi3.NewObjectSchema().
				WithProperty("a", openapi3.NewStringSchema()).
				WithProperty("b", openapi3.NewIntegerSchema()).
				WithProperty("c", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())).
				WithProperty("d", openapi3.NewObjectSchema().WithProperty("d1", openapi3.NewStringSchema())).
				WithProperty("f", openapi3.NewStringSchema().WithFormat("binary")).
				WithProperty("g", openapi3.NewStringSchema()),
			want: map[string]interface{}{"a": "a1", "b": float64(10), "c": []interface{}{"c1", "c2"}, "d": map[string]interface{}{"d1": "d1"}, "f": "foo", "g": "g1"},
		},
		{
			name: "multipartExtraPart",
			mime: multipartFormMimeExtraPart,
			body: multipartFormExtraPart,
			schema: openapi3.NewObjectSchema().
				WithProperty("a", openapi3.NewStringSchema()),
			want:    map[string]interface{}{"a": "a1"},
			wantErr: &ParseError{Kind: KindOther},
		},
		{
			name: "multipartAnyAdditionalProperties",
			mime: multipartMimeAnyAdditionalProps,
			body: multipartAnyAdditionalProps,
			schema: openapi3.NewObjectSchema().
				WithAnyAdditionalProperties().
				WithProperty("a", openapi3.NewStringSchema()),
			want: map[string]interface{}{"a": "a1"},
		},
		{
			name: "multipartWithAdditionalProperties",
			mime: multipartMimeAdditionalProps,
			body: multipartAdditionalProps,
			schema: openapi3.NewObjectSchema().
				WithAdditionalProperties(openapi3.NewObjectSchema().
					WithProperty("x", openapi3.NewStringSchema())).
				WithProperty("a", openapi3.NewStringSchema()),
			want: map[string]interface{}{"a": "a1", "x": "x1"},
		},
		{
			name: "multipartWithAdditionalPropertiesError",
			mime: multipartMimeAdditionalPropsErr,
			body: multipartAdditionalPropsErr,
			schema: openapi3.NewObjectSchema().
				WithAdditionalProperties(openapi3.NewObjectSchema().
					WithProperty("x", openapi3.NewStringSchema())).
				WithProperty("a", openapi3.NewStringSchema()),
			want:    map[string]interface{}{"a": "a1", "x": "x1"},
			wantErr: &ParseError{Kind: KindOther},
		},
		{
			name: "file",
			mime: "application/octet-stream",
			body: strings.NewReader("foo"),
			want: "foo",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := make(http.Header)
			h.Set(headerCT, tc.mime)
			var schemaRef *openapi3.SchemaRef
			if tc.schema != nil {
				schemaRef = tc.schema.NewRef()
			}
			encFn := func(name string) *openapi3.Encoding {
				if tc.encoding == nil {
					return nil
				}
				return tc.encoding[name]
			}
			got, err := decodeBody(tc.body, h, schemaRef, encFn)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.Truef(t, matchParseError(err, tc.wantErr), "got error:\n%v\nwant error:\n%v", err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Truef(t, reflect.DeepEqual(got, tc.want), "got %v, want %v", got, tc.want)
		})
	}
}

type testFormPart struct {
	name        string
	contentType string
	data        io.Reader
	filename    string
}

func newTestMultipartForm(parts []*testFormPart) (io.Reader, string, error) {
	form := &bytes.Buffer{}
	w := multipart.NewWriter(form)
	defer w.Close()

	for _, p := range parts {
		var disp string
		if p.filename == "" {
			disp = fmt.Sprintf("form-data; name=%q", p.name)
		} else {
			disp = fmt.Sprintf("form-data; name=%q; filename=%q", p.name, p.filename)
		}

		h := make(textproto.MIMEHeader)
		h.Set(headerCT, p.contentType)
		h.Set("Content-Disposition", disp)
		pw, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		if _, err = io.Copy(pw, p.data); err != nil {
			return nil, "", err
		}
	}
	return form, w.FormDataContentType(), nil
}

func TestRegisterAndUnregisterBodyDecoder(t *testing.T) {
	var decoder BodyDecoder
	decoder = func(body io.Reader, h http.Header, schema *openapi3.SchemaRef, encFn EncodingFn) (decoded interface{}, err error) {
		var data []byte
		if data, err = ioutil.ReadAll(body); err != nil {
			return
		}
		return strings.Split(string(data), ","), nil
	}
	contentType := "text/csv"
	h := make(http.Header)
	h.Set(headerCT, contentType)

	originalDecoder := RegisteredBodyDecoder(contentType)
	require.Nil(t, originalDecoder)

	RegisterBodyDecoder(contentType, decoder)
	require.Equal(t, fmt.Sprintf("%v", decoder), fmt.Sprintf("%v", RegisteredBodyDecoder(contentType)))

	body := strings.NewReader("foo,bar")
	schema := openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema()).NewRef()
	encFn := func(string) *openapi3.Encoding { return nil }
	got, err := decodeBody(body, h, schema, encFn)

	require.NoError(t, err)
	require.Equal(t, []string{"foo", "bar"}, got)

	UnregisterBodyDecoder(contentType)

	originalDecoder = RegisteredBodyDecoder(contentType)
	require.Nil(t, originalDecoder)

	_, err = decodeBody(body, h, schema, encFn)
	require.Equal(t, &ParseError{
		Kind:   KindUnsupportedFormat,
		Reason: prefixUnsupportedCT + ` "text/csv"`,
	}, err)
}

func matchParseError(got, want error) bool {
	wErr, ok := want.(*ParseError)
	if !ok {
		return false
	}
	gErr, ok := got.(*ParseError)
	if !ok {
		return false
	}
	if wErr.Kind != gErr.Kind {
		return false
	}
	if !reflect.DeepEqual(wErr.Value, gErr.Value) {
		return false
	}
	if !reflect.DeepEqual(wErr.Path(), gErr.Path()) {
		return false
	}
	if wErr.Cause != nil {
		return matchParseError(gErr.Cause, wErr.Cause)
	}
	return true
}
