package desktop

import (
	"context"
	"errors"
	"testing"
)

func TestChooseExternalDSH(t *testing.T) {
	for _, test := range []struct {
		name    string
		answers []bool
		want    externalDSHChoice
	}{
		{"stop", []bool{true}, externalDSHKill},
		{"keep", []bool{false, true}, externalDSHOtherPort},
		{"cancel", []bool{false, false}, externalDSHCancel},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			got, err := chooseExternalDSH(context.Background(), "existing instances", func(context.Context, string) (bool, error) {
				answer := test.answers[calls]
				calls++
				return answer, nil
			})
			if err != nil || got != test.want || calls != len(test.answers) {
				t.Fatalf("choice=%v, calls=%d, err=%v", got, calls, err)
			}
		})
	}
}

func TestChooseExternalDSHStopsAfterShutdown(t *testing.T) {
	calls := 0
	_, err := chooseExternalDSH(context.Background(), "existing instances", func(context.Context, string) (bool, error) {
		calls++
		return false, context.Canceled
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
}

func TestWindowsQuestionUsesSupportedCallbackLabels(t *testing.T) {
	yes, no := questionButtonLabels("windows")
	if yes != "Yes" || no != "No" {
		t.Fatalf("unsupported Windows callback labels: %q, %q", yes, no)
	}
}
