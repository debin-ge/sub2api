package service

import "testing"

func TestValidateEmailLocalPart(t *testing.T) {
	cases := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"plain email", "user@gmail.com", false},
		{"plus addressing", "user+1@gmail.com", true},
		{"plus with tag", "wangtaosxau+a@gmail.com", true},
		{"dot in local part", "user.name@gmail.com", false},
		{"multiple dots local", "u.s.e.r@gmail.com", false},
		{"plus and dot combined", "u.ser+promo@gmail.com", true},
		{"dot only in domain is allowed", "user@mail.example.com", false},
		{"subdomain domain allowed", "abc@sub.domain.co.uk", false},
		{"uppercase normalized", "User@Gmail.com", false},
		{"leading/trailing spaces", "  user@gmail.com  ", false},
		{"plus after normalization", "  User+x@Gmail.com ", true},
		{"invalid no domain", "usersonly", true},
		{"invalid empty local", "@gmail.com", true},
		{"invalid empty domain", "user@", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmailLocalPart(tc.email)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateEmailLocalPart(%q): expected error, got nil", tc.email)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateEmailLocalPart(%q): expected no error, got %v", tc.email, err)
			}
		})
	}
}
