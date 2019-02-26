package langserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrettyPrintTypesString(t *testing.T) {
	type TestCase struct {
		input    string
		expected string
	}

	testCases := []TestCase{
		TestCase{
			input: `struct{Model; Name string; Age int64; Birthday Time; MemberNumber string "gorm:\"unique;not null\""}`,
			expected: `struct {
    Model
    Name string
    Age int64
    Birthday Time
    MemberNumber string ` + "`gorm:\"unique;not null\"`" + `
}`,
		},
	}

	for _, testCase := range testCases {
		require := require.New(t)

		actual := prettyPrintTypesString(testCase.input)
		require.Equal(testCase.expected, actual)
	}
}
