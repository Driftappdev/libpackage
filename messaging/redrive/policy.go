package redrive

type Policy struct { DeleteOnSuccess bool; MaxBatch int }

func DefaultPolicy() Policy { return Policy{DeleteOnSuccess: false, MaxBatch: 100} }
