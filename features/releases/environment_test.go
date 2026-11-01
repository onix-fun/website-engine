package releases

import (
	"errors"
	"strings"
	"testing"

	"websiteengine/pkg/environment"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"  Production  ":         "Production",
		"multi   space":          "multi space",
		"":                       "",
		"   ":                    "",
		strings.Repeat("ё", 250): strings.Repeat("ё", 200), // runes, not bytes
	}
	for raw, want := range cases {
		if got := sanitizeName(raw); got != want {
			t.Errorf("sanitizeName(%q): got %q want %q", raw, got, want)
		}
	}
}

func TestUpdateEnvironmentValidation(t *testing.T) {
	// The service is built without a database: every case below must fail on
	// validation before the repository is touched.
	svc := NewService(nil, nil, nil, 0)
	env := environment.Dev

	metaBad := func(v any) *[]string { x := v.([]string); return &x }

	tests := []struct {
		name    string
		env     environment.Environment
		apply   func(in *EnvironmentInput)
		wantErr string
	}{
		{name: "invalid env", env: environment.Environment("stage"), wantErr: "environment must be dev or prod"},
		{name: "empty name", env: env, apply: func(in *EnvironmentInput) {
			s := "   "
			in.Name = &s
		}, wantErr: "name must be 1-200 characters"},
		{name: "bad base url", env: env, apply: func(in *EnvironmentInput) {
			s := "not a url"
			in.PublicBaseURL = &s
		}, wantErr: "public_base_url must be an absolute"},
		{name: "relative base url", env: env, apply: func(in *EnvironmentInput) {
			s := "/api"
			in.PublicBaseURL = &s
		}, wantErr: "public_base_url must be an absolute"},
		{name: "too many origins", env: env, apply: func(in *EnvironmentInput) {
			s := make([]string, 0, corsOriginLimit+1)
			for i := 0; i <= corsOriginLimit; i++ {
				s = append(s, "https://origin.example.com/"+strings.Repeat("a", i)+"")
			}
			in.CORSOrigins = metaBad(s)
		}, wantErr: "at most"},
		{name: "origin not a url", env: env, apply: func(in *EnvironmentInput) {
			s := []string{"javascript:alert(1)"}
			in.CORSOrigins = metaBad(s)
		}, wantErr: "must be an absolute http(s) URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := EnvironmentInput{Revision: 1}
			if tc.apply != nil {
				tc.apply(&in)
			}
			_, err := svc.UpdateEnvironment(t.Context(), tc.env, in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation wrapping, got %v", err)
			}
		})
	}
}

func TestValidationErrorUnwraps(t *testing.T) {
	var err error = &ValidationError{Message: "boom"}
	if !errors.Is(err, ErrValidation) {
		t.Fatal("ValidationError must unwrap to ErrValidation")
	}
	conflict := &envConflict{current: 7}
	if !errors.Is(conflict, ErrRevisionConflict) {
		t.Fatal("envConflict must unwrap to ErrRevisionConflict")
	}
	rev, ok := CurrentRevision(conflict)
	if !ok || rev != 7 {
		t.Fatalf("CurrentRevision = %d, %v", rev, ok)
	}
	foreign := errors.New("other")
	if _, ok := CurrentRevision(foreign); ok {
		t.Fatal("foreign error must not report a revision")
	}
}
