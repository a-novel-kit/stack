package cli

import "testing"

func TestValidateBotCommentArgs(t *testing.T) {
	t.Parallel()

	// One const so the repeated literal does not trip goconst.
	const testRepo = "service-authentication"

	testCases := []struct {
		name string

		org    string
		repo   string
		number string

		expectErr bool
	}{
		{
			name:   "valid a-novel target",
			org:    orgAnovel,
			repo:   testRepo,
			number: "549",
		},
		{
			name:   "valid a-novel-kit target",
			org:    orgAnovelKit,
			repo:   "golib",
			number: "1",
		},
		{
			name:      "unknown org",
			org:       "a-other",
			repo:      testRepo,
			number:    "549",
			expectErr: true,
		},
		{
			name:      "empty repo",
			org:       orgAnovel,
			repo:      "",
			number:    "549",
			expectErr: true,
		},
		{
			name:      "repo carries org prefix",
			org:       orgAnovel,
			repo:      orgAnovel + "/" + testRepo,
			number:    "549",
			expectErr: true,
		},
		{
			name:      "non-numeric number",
			org:       orgAnovel,
			repo:      testRepo,
			number:    "abc",
			expectErr: true,
		},
		{
			name:      "zero number",
			org:       orgAnovel,
			repo:      testRepo,
			number:    "0",
			expectErr: true,
		},
		{
			name:      "negative number",
			org:       orgAnovel,
			repo:      testRepo,
			number:    "-3",
			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateBotCommentArgs(testCase.org, testCase.repo, testCase.number)
			if testCase.expectErr && err == nil {
				t.Fatalf("expected an error for org=%q repo=%q number=%q, got nil",
					testCase.org, testCase.repo, testCase.number)
			}
			if !testCase.expectErr && err != nil {
				t.Fatalf("expected no error for org=%q repo=%q number=%q, got: %v",
					testCase.org, testCase.repo, testCase.number, err)
			}
		})
	}
}
