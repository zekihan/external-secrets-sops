package decrypt

import (
	"context"
	"errors"

	"github.com/getsops/sops/v3/decrypt"
)

type SOPS struct{}

func (SOPS) Decrypt(ctx context.Context, encrypted []byte) (plain []byte, err error) {
	defer func() {
		if recover() != nil {
			plain = nil
			err = errors.New("SOPS backend failed")
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := (SOPS{}).ValidateCandidate(ctx, [][]byte{encrypted}); err != nil {
		return nil, err
	}
	plain, err = decrypt.Data(encrypted, "yaml")
	if err != nil {
		return nil, errors.New("SOPS decryption or integrity verification failed")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return plain, nil
}
