package langserver

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"

	"github.com/saibing/bingo/langserver/internal/util"
)

func TestCompletion(t *testing.T) {
	test := func(t *testing.T, input string, output string) {
		testCompletion(t, &completionTestCase{input: input, output: output})
	}

	t.Run("basic", func(t *testing.T) {
		test(t, "basic/b.go:1:24", "1:23-1:24 A() function ")
	})

	t.Run("xtest", func(t *testing.T) {
		test(t, "xtest/x_test.go:1:87", "1:86-1:87 p module \"github.com/saibing/bingo/langserver/test/pkg/xtest\", panic(interface{}) function , print(args ...T) function , println(args ...T) function ")
		test(t, "xtest/x_test.go:1:88", "1:88-1:88 A variable int, X variable int, Y() function int")
		test(t, "xtest/b_test.go:1:35", "1:34-1:35 X variable int")
	})

	t.Run("go subdirectory in repo", func(t *testing.T) {
		test(t, "subdirectory/d2/b.go:1:94", "1:94-1:94 A() function ")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:21", "1:21-1:21 fmt module \"fmt\", x variable int, append(slice []T, elems ...T) function []T, bool typeParameter , byte typeParameter , cap(v []T) function int, close(c chan<- T) function , complex(real, imag float64) function complex128, complex128 typeParameter , complex64 typeParameter , copy(dst, src []T) function int, delete(m map[K]V, key K) function , error interface , false constant , float32 typeParameter , float64 typeParameter , imag(complex128) function float64, int typeParameter , int16 typeParameter , int32 typeParameter , int64 typeParameter , int8 typeParameter , iota constant , len(T) function int, make(t T, size ...int) function T, new(T) function *T, nil variable , panic(interface{}) function , print(args ...T) function , println(args ...T) function , real(complex128) function float64, recover() function interface{}, rune typeParameter , string typeParameter , true constant , uint typeParameter , uint16 typeParameter , uint32 typeParameter , uint64 typeParameter , uint8 typeParameter , uintptr typeParameter ")
		test(t, "goroot/a.go:1:44", "1:38-1:44 Println(a ...interface{}) function n int, err error")
	})

	t.Run("go project workspace", func(t *testing.T) {
		test(t, "goproject/b/b.go:1:87", "1:87-1:87 a module \"github.com/saibing/bingo/langserver/test/pkg/goproject/a\", append(slice []T, elems ...T) function []T, bool typeParameter , byte typeParameter , cap(v []T) function int, close(c chan<- T) function , complex(real, imag float64) function complex128, complex128 typeParameter , complex64 typeParameter , copy(dst, src []T) function int, delete(m map[K]V, key K) function , error interface , false constant , float32 typeParameter , float64 typeParameter , imag(complex128) function float64, int typeParameter , int16 typeParameter , int32 typeParameter , int64 typeParameter , int8 typeParameter , iota constant , len(T) function int, make(t T, size ...int) function T, new(T) function *T, nil variable , panic(interface{}) function , print(args ...T) function , println(args ...T) function , real(complex128) function float64, recover() function interface{}, rune typeParameter , string typeParameter , true constant , uint typeParameter , uint16 typeParameter , uint32 typeParameter , uint64 typeParameter , uint8 typeParameter , uintptr typeParameter ")
		test(t, "goproject/b/b.go:1:89", "1:89-1:89 A() function ")
	})

	t.Run("go module dep", func(t *testing.T) {
		test(t, "gomodule/a.go:1:40", "1:40-1:40 dep module \"github.com/saibing/dep\", append(slice []T, elems ...T) function []T, bool typeParameter , byte typeParameter , cap(v []T) function int, close(c chan<- T) function , complex(real, imag float64) function complex128, complex128 typeParameter , complex64 typeParameter , copy(dst, src []T) function int, delete(m map[K]V, key K) function , error interface , false constant , float32 typeParameter , float64 typeParameter , imag(complex128) function float64, int typeParameter , int16 typeParameter , int32 typeParameter , int64 typeParameter , int8 typeParameter , iota constant , len(T) function int, make(t T, size ...int) function T, new(T) function *T, nil variable , panic(interface{}) function , print(args ...T) function , println(args ...T) function , real(complex128) function float64, recover() function interface{}, rune typeParameter , string typeParameter , true constant , uint typeParameter , uint16 typeParameter , uint32 typeParameter , uint64 typeParameter , uint8 typeParameter , uintptr typeParameter ")
		test(t, "gomodule/a.go:1:57", "1:57-1:57 D() function ")

		test(t, "gomodule/b.go:1:40", "1:40-1:40 subp module \"github.com/saibing/dep/subp\", append(slice []T, elems ...T) function []T, bool typeParameter , byte typeParameter , cap(v []T) function int, close(c chan<- T) function , complex(real, imag float64) function complex128, complex128 typeParameter , complex64 typeParameter , copy(dst, src []T) function int, delete(m map[K]V, key K) function , error interface , false constant , float32 typeParameter , float64 typeParameter , imag(complex128) function float64, int typeParameter , int16 typeParameter , int32 typeParameter , int64 typeParameter , int8 typeParameter , iota constant , len(T) function int, make(t T, size ...int) function T, new(T) function *T, nil variable , panic(interface{}) function , print(args ...T) function , println(args ...T) function , real(complex128) function float64, recover() function interface{}, rune typeParameter , string typeParameter , true constant , uint typeParameter , uint16 typeParameter , uint32 typeParameter , uint64 typeParameter , uint8 typeParameter , uintptr typeParameter ")
		test(t, "gomodule/b.go:1:63", "1:63-1:63 D() function ")

		test(t, "gomodule/c.go:1:68", "1:68-1:68 D2 field int")
	})

	t.Run("completion", func(t *testing.T) {
		test(t, "completion/a.go:6:7", "6:6-6:7 strings module \"strings\", s1 = 42 constant int, s2() function , s3 variable int, s4 variable func(), string typeParameter ")
		test(t, "completion/a.go:7:7", "7:6-7:7 new(T) function *T, nil variable ")
		test(t, "completion/a.go:12:11", "12:8-12:11 int typeParameter , int16 typeParameter , int32 typeParameter , int64 typeParameter , int8 typeParameter ")
		test(t, "completion/b.go:1:44", "1:38-1:44 Println(a ...interface{}) function n int, err error")
		test(t, "completion/c.go:8:11", "1:38-1:44 Println(a ...interface{}) function n int, err error")
	})
}

type completionTestCase struct {
	input  string
	output string
}

func testCompletion(tb testing.TB, c *completionTestCase) {
	tbRun(tb, fmt.Sprintf("complete-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testCompletion", err)
		}
		doCompletionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doCompletionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := callCompletion(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if completion != want {
		t.Fatalf("\ngot %q, \nwant %q", completion, want)
	}
}

func callCompletion(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res lsp.CompletionList
	err := c.Call(ctx, "textDocument/completion", lsp.CompletionParams{TextDocumentPositionParams: lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, it := range res.Items {
		if i != 0 {
			str += ", "
		} else {
			e := it.TextEdit.Range
			str += fmt.Sprintf("%d:%d-%d:%d ", e.Start.Line+1, e.Start.Character+1, e.End.Line+1, e.End.Character+1)
		}
		str += fmt.Sprintf("%s %s %s", it.Label, it.Kind, it.Detail)
	}
	return str, nil
}
