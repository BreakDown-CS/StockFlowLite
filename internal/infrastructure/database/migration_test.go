package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMigrationFilename(t *testing.T) {
	tests := []struct {
		filename    string
		suffix      string
		wantVersion string
		wantName    string
	}{
		{
			filename:    "000001_create_users.up.sql",
			suffix:      ".up.sql",
			wantVersion: "000001",
			wantName:    "create_users",
		},
		{
			filename:    "000002_create_products.down.sql",
			suffix:      ".down.sql",
			wantVersion: "000002",
			wantName:    "create_products",
		},
		{
			filename:    "invalid_file.txt",
			suffix:      ".up.sql",
			wantVersion: "",
			wantName:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			v, n := parseMigrationFilename(tt.filename, tt.suffix)
			assert.Equal(t, tt.wantVersion, v)
			assert.Equal(t, tt.wantName, n)
		})
	}
}
