package tx

import "errors"

var (
    ErrAlreadyInTx      = errors.New("tx: transaction already active")
    ErrNoActiveTx       = errors.New("tx: no active transaction")
    ErrInvalidExecutor  = errors.New("tx: invalid executor")
    ErrRollbackOnly     = errors.New("tx: transaction marked rollback-only")
    ErrUnsupportedLevel = errors.New("tx: unsupported isolation level")
)
