package release

import (
	"errors"
	"strings"
	"testing"
)

func TestInputsValidate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		create  *CreateInput
		update  *UpdateInput
		asset   *AssetInput
		wantErr error
	}{
		{name: "create valid", create: &CreateInput{TagName: "v1.0.0", Name: "Version 1", CreatedByDID: "did:plc:alice"}},
		{name: "create invalid tag", create: &CreateInput{TagName: "../main", Name: "Version 1", CreatedByDID: "did:plc:alice"}, wantErr: ErrValidation},
		{name: "create oversized body", create: &CreateInput{TagName: "v1", Name: "Version 1", Body: strings.Repeat("x", maxReleaseBodyBytes+1), CreatedByDID: "did:plc:alice"}, wantErr: ErrValidation},
		{name: "update valid", update: &UpdateInput{Name: "Version 2"}},
		{name: "update blank name", update: &UpdateInput{}, wantErr: ErrValidation},
		{name: "asset valid", asset: &AssetInput{Name: "adenosine.tar.gz", ContentType: "application/gzip", SizeBytes: 4, Body: strings.NewReader("data")}},
		{name: "asset path rejected", asset: &AssetInput{Name: "../asset", ContentType: "application/octet-stream", Body: strings.NewReader("")}, wantErr: ErrValidation},
		{name: "asset invalid media type", asset: &AssetInput{Name: "asset", ContentType: "binary", Body: strings.NewReader("")}, wantErr: ErrValidation},
		{name: "asset over quota", asset: &AssetInput{Name: "asset", ContentType: "application/octet-stream", SizeBytes: 11, Body: strings.NewReader("data")}, wantErr: ErrQuotaExceeded},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var err error
			switch {
			case testCase.create != nil:
				err = testCase.create.Validate()
			case testCase.update != nil:
				err = testCase.update.Validate()
			case testCase.asset != nil:
				_, err = testCase.asset.Validate(10)
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestLimitsValidate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		limits  Limits
		wantErr error
	}{
		{name: "valid", limits: Limits{AssetBytes: 1, ReleaseBytes: 2, RepositoryBytes: 3}},
		{name: "zero", limits: Limits{}, wantErr: ErrValidation},
		{name: "release below asset", limits: Limits{AssetBytes: 2, ReleaseBytes: 1, RepositoryBytes: 3}, wantErr: ErrValidation},
		{name: "repository below release", limits: Limits{AssetBytes: 1, ReleaseBytes: 3, RepositoryBytes: 2}, wantErr: ErrValidation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.limits.Validate(); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}
