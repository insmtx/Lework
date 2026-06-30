package opencode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/insmtx/Leros/backend/agent/runtime/events"
)

const maxPlanFileSize = 256 * 1024

var planQuestionPathPattern = regexp.MustCompile(`^Plan at (.+) is complete\.`)

func (st *runState) planHandoff(questions []events.QuestionItem) *events.PlanHandoffPayload {
	handoff := &events.PlanHandoffPayload{}
	path, displayPath, err := st.resolvePlanPath(questions)
	handoff.FilePath = displayPath
	if err != nil {
		handoff.Error = err.Error()
		return handoff
	}

	content, err := readPlanFile(path)
	if err != nil {
		handoff.Error = fmt.Sprintf("read plan file: %v", err)
		return handoff
	}
	handoff.Content = content
	return handoff
}

func (st *runState) resolvePlanPath(questions []events.QuestionItem) (string, string, error) {
	expectedName := ""
	if st.session != nil && st.session.Slug != "" && st.session.Time.Created > 0 {
		expectedName = fmt.Sprintf("%d-%s.md", st.session.Time.Created, st.session.Slug)
	}

	baseDir := st.workDir
	if strings.TrimSpace(baseDir) == "" && st.session != nil && strings.TrimSpace(st.session.Directory) != "" {
		baseDir = st.session.Directory
	}

	questionPath := extractPlanPath(questions)
	if questionPath != "" {
		path := questionPath
		if !filepath.IsAbs(path) {
			if strings.TrimSpace(baseDir) == "" {
				return "", questionPath, errors.New("plan file base directory is unavailable")
			}
			path = filepath.Join(baseDir, path)
		}
		path = filepath.Clean(path)
		if err := validatePlanPath(path, expectedName); err != nil {
			return "", questionPath, err
		}
		return path, questionPath, nil
	}

	if expectedName == "" || strings.TrimSpace(baseDir) == "" {
		return "", "", errors.New("plan file path is unavailable")
	}
	path := filepath.Join(baseDir, ".opencode", "plans", expectedName)
	displayPath := filepath.Join(".opencode", "plans", expectedName)
	return path, displayPath, nil
}

func validatePlanPath(path, expectedName string) error {
	if filepath.Ext(path) != ".md" || filepath.Base(filepath.Dir(path)) != "plans" {
		return errors.New("plan file path is invalid")
	}
	if expectedName != "" && filepath.Base(path) != expectedName {
		return errors.New("plan file does not match the current session")
	}
	return nil
}

func extractPlanPath(questions []events.QuestionItem) string {
	for _, question := range questions {
		match := planQuestionPathPattern.FindStringSubmatch(strings.TrimSpace(question.Question))
		if len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func readPlanFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxPlanFileSize+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxPlanFileSize {
		return "", errors.New("plan file exceeds the size limit")
	}
	if strings.TrimSpace(string(content)) == "" {
		return "", errors.New("plan file is empty")
	}
	return string(content), nil
}
