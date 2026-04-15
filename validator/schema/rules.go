package schema

type Rule func(any) error

type Validator struct{ rules []Rule }

func New(rules ...Rule) *Validator { return &Validator{rules: rules} }

func (v *Validator) Validate(value any) error {
	for _, rule := range v.rules {
		if err := rule(value); err != nil {
			return err
		}
	}
	return nil
}
