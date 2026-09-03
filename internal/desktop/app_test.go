package desktop

import "testing"

func TestNormaliseWindowState(t *testing.T) {
	tests := []struct {
		name  string
		input windowState
		want  windowState
	}{
		{
			name:  "valid state is preserved",
			input: windowState{X: -1200, Y: 30, Width: 1440, Height: 900, Maximised: true, HasPosition: true},
			want:  windowState{X: -1200, Y: 30, Width: 1440, Height: 900, Maximised: true, HasPosition: true},
		},
		{
			name:  "invalid dimensions use defaults",
			input: windowState{Width: 10, Height: 20000},
			want:  windowState{Width: defaultWidth, Height: defaultHeight},
		},
		{
			name:  "implausible position is discarded",
			input: windowState{X: 100001, Y: 20, Width: 900, Height: 600, HasPosition: true},
			want:  windowState{Width: 900, Height: 600},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normaliseWindowState(test.input)
			if got != test.want {
				t.Fatalf("normaliseWindowState() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSetEnvironmentReplacesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"PATH=/bin", "DSH_HOME=old", "OTHER=value"}, "DSH_HOME", "new")
	want := []string{"PATH=/bin", "OTHER=value", "DSH_HOME=new"}
	if len(got) != len(want) {
		t.Fatalf("setEnvironment() length = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("setEnvironment()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
