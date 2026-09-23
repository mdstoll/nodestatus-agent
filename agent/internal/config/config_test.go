package config

import "testing"

func TestStripInlineComment(t *testing.T) {
	cases := map[string]string{
		`"Pi #2"`:             `"Pi #2"`,
		`"Pi #2" # de tweede`: `"Pi #2"`,
		`29500 # poort`:       `29500`,
		`'a # b'`:             `'a # b'`,
		`["a", "b"] # lijst`:  `["a", "b"]`,
		`abc#def`:             `abc#def`,
		`# alleen commentaar`: ``,
	}
	for in, want := range cases {
		if got := stripInlineComment(in); got != want {
			t.Errorf("stripInlineComment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateClampsEnrollment(t *testing.T) {
	c := Default()
	c.Bind = "127.0.0.1:29500"
	c.EnrollWindowMinutes, c.EnrollMaxAttempts = 0, -1
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.EnrollWindowMinutes != 15 || c.EnrollMaxAttempts != 5 {
		t.Errorf("got window=%d attempts=%d, want 15 and 5", c.EnrollWindowMinutes, c.EnrollMaxAttempts)
	}
}
