package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// SkillStringList stores assistant Skill names as a JSON array.
type SkillStringList []string

// Scan implements sql.Scanner.
func (s *SkillStringList) Scan(value interface{}) error {
	if value == nil {
		*s = SkillStringList{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into SkillStringList", value)
	}
	var result []string
	if err := json.Unmarshal(bytes, &result); err != nil {
		return err
	}
	*s = SkillStringList(result)
	return nil
}

// Value implements driver.Valuer.
func (s SkillStringList) Value() (driver.Value, error) {
	if len(s) == 0 {
		return nil, nil
	}
	return json.Marshal([]string(s))
}
