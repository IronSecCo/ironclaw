package main

import "testing"

// TestRunFirstRunHelp covers the F10 first-run UX fix (IRO-39): bare `ironctl`
// and explicit `help` greet the user and succeed (exit 0), while genuine misuse
// still fails (exit non-zero). None of these paths touch the network.
func TestRunFirstRunHelp(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no args", nil, false},
		{"only global flags", []string{"--addr", "http://127.0.0.1:9"}, false},
		{"help", []string{"help"}, false},
		{"--help", []string{"--help"}, false},
		{"-h", []string{"-h"}, false},
		{"unknown command is misuse", []string{"frobnicate"}, true},
		{"change without verb is misuse", []string{"change"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args)
			if tc.wantErr && err == nil {
				t.Fatalf("run(%v): want error, got nil", tc.args)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("run(%v): want nil, got %v", tc.args, err)
			}
		})
	}
}
