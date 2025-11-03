package jsonedit_test

import (
	"strings"
	"testing"

	jsonedit "github.com/tsukinoko-kun/jsonedit"
)

type (
	TestData interface {
		SetDependency(name, version string)
		DeleteDevDependency(name string)
	}

	SimpleStruct struct {
		Foo string `json:"foo"`
		Bar int    `json:"bar"`
		Baz bool
	}

	PackageJson struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
)

func (s *SimpleStruct) SetDependency(name, version string) {
}

func (s *SimpleStruct) DeleteDevDependency(name string) {
}

func (s *PackageJson) SetDependency(name, version string) {
	if s.Dependencies == nil {
		s.Dependencies = make(map[string]string)
	}
	s.Dependencies[name] = version
}

func (s *PackageJson) DeleteDevDependency(name string) {
	if s.DevDependencies == nil {
		s.DevDependencies = make(map[string]string)
	}
	delete(s.DevDependencies, name)
}

type TestCase[T any] struct {
	name      string // description of this test case
	r         string
	typedData T
	want      string
	wantErr   bool
	operation func(data T)
}

func TestParse(t *testing.T) {
	tests := []TestCase[TestData]{
		{
			name:      "no changes 1",
			r:         `{"foo": "bar", "bar": 42, "Baz": true}`,
			typedData: &SimpleStruct{},
			want:      `{"foo": "bar", "bar": 42, "Baz": true}`,
			wantErr:   false,
		},
		{
			name:      "no changes 2",
			r:         `{"foo": "bar", "bar": 42, "Baz": true, "qux": 69}`,
			typedData: &SimpleStruct{},
			want:      `{"foo": "bar", "bar": 42, "Baz": true, "qux": 69}`,
			wantErr:   false,
		},
		{
			name:      "no changes 3",
			r:         `{"foo": "bar", "qux": 69, "bar": 42, "Baz": true}`,
			typedData: &SimpleStruct{},
			want:      `{"foo": "bar", "qux": 69, "bar": 42, "Baz": true}`,
			wantErr:   false,
		},
		{
			name: "realistic package.json",
			r: `{
  "name": "json-edit",
  "version": "0.1.0",
  "description": "JSON editing library",
  "main": "jsonedit.go",
  "scripts": {
    "test": "go test"
  },
  "dependencies": {
    "github.com/stretchr/testify": "^1.8.4",
    "github.com/tsukinoko-kun/json-edit": "0.1.0"
  },
  "devDependencies": {
    "eslint": "^8.46.0",
    "eslint-config-prettier": "^8.9.0",
    "eslint-plugin-prettier": "^5.0.0",
    "prettier": "^3.0.0"
  }
}
`,
			typedData: &PackageJson{},
			operation: func(pkg TestData) {
				pkg.SetDependency("zod", "^3.21.4")
				pkg.DeleteDevDependency("prettier")
				pkg.DeleteDevDependency("eslint-config-prettier")
				pkg.DeleteDevDependency("eslint-plugin-prettier")
			},
			want: `{
  "name": "json-edit",
  "version": "0.1.0",
  "description": "JSON editing library",
  "main": "jsonedit.go",
  "scripts": {
    "test": "go test"
  },
  "dependencies": {
    "github.com/stretchr/testify": "^1.8.4",
    "github.com/tsukinoko-kun/json-edit": "0.1.0",
    "zod": "^3.21.4"
  },
  "devDependencies": {
    "eslint": "^8.46.0"
  }
}
`,
			wantErr: false,
		},
		{
			name: "package.json create dependencies key",
			r: `{
  "name": "json-edit",
  "version": "0.1.0",
  "description": "JSON editing library",
  "main": "jsonedit.go",
  "scripts": {
    "test": "go test"
  },
  "devDependencies": {
    "eslint": "^8.46.0",
    "prettier": "^3.0.0"
  }
}
`,
			typedData: &PackageJson{},
			operation: func(pkg TestData) {
				pkg.SetDependency("zod", "^3.21.4")
			},
			want: `{
  "name": "json-edit",
  "version": "0.1.0",
  "description": "JSON editing library",
  "main": "jsonedit.go",
  "scripts": {
    "test": "go test"
  },
  "dependencies": {
    "zod": "^3.21.4"
  },
  "devDependencies": {
    "eslint": "^8.46.0",
    "prettier": "^3.0.0"
  }
}
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := jsonedit.Parse(strings.NewReader(tt.r), tt.typedData)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Parse() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("Parse() succeeded unexpectedly")
			}
			if tt.operation != nil {
				tt.operation(got.TypedData)
			}
			gotStr, gotErr := got.String()
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("String() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("String() succeeded unexpectedly")
			}
			if gotStr != tt.want {
				t.Errorf("Got %q want %q", gotStr, tt.want)
			}
		})
	}
}
