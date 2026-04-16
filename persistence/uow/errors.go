package uow

import "errors"

var (
    ErrFactoryNotFound = errors.New("uow: repository factory not found")
    ErrDuplicateFactory = errors.New("uow: repository factory already registered")
    ErrNilRepository = errors.New("uow: nil repository")
)
