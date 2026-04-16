package redrive

type Request struct {
    IDs           []string
    DryRun        bool
    OverrideTopic string
}
