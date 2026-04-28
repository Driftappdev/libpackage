package config

type Loader[T any] interface{ Load() (T, error) }
