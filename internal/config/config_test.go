package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	valid := Defaults()
	valid.Target = "i-bastion"
	valid.LocalPort = "3306"
	valid.RemotePort = "5432"

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "valid", cfg: valid},
		{name: "missing values", cfg: Defaults(), want: `required flag(s) "target", "local-port", "remote-port" not set`},
		{name: "non-numeric port", cfg: Config{Target: "i-bastion", LocalPort: "invalid", RemotePort: "3306"}, want: "--local-port must be an integer between 1 and 65535"},
		{name: "port outside range", cfg: Config{Target: "i-bastion", LocalPort: "65536", RemotePort: "3306"}, want: "--local-port must be an integer between 1 and 65535"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cfg.Validate()
			if test.want == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, test.want)
			}
		})
	}
}
