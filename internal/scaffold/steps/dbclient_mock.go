package steps

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// MockDatabaseClient implements DatabaseClient for testing.
type MockDatabaseClient struct {
	mu sync.Mutex

	databases   map[string]bool
	createCalls []string
	dropCalls   []string
	listCalls   []string

	pingError   error
	createError error
	dropError   error
	listError   error
	closeError  error

	dropErrors  map[string]error
	listErrors  map[string]error
	listResults map[string][]string

	existsOnCall int
	callCount    int
}

func NewMockDatabaseClient() *MockDatabaseClient {
	return &MockDatabaseClient{
		databases:   make(map[string]bool),
		createCalls: make([]string, 0),
		dropCalls:   make([]string, 0),
		listCalls:   make([]string, 0),
		dropErrors:  make(map[string]error),
		listErrors:  make(map[string]error),
		listResults: make(map[string][]string),
	}
}

func (m *MockDatabaseClient) Ping() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pingError
}

func (m *MockDatabaseClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeError
}

func (m *MockDatabaseClient) CreateDatabase(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.createCalls = append(m.createCalls, name)
	m.callCount++
	if m.createError != nil {
		return m.createError
	}
	if m.existsOnCall > 0 && m.callCount <= m.existsOnCall {
		return &DatabaseExistsError{Name: name}
	}
	if m.databases[name] {
		return &DatabaseExistsError{Name: name}
	}
	m.databases[name] = true
	return nil
}

func (m *MockDatabaseClient) DropDatabase(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dropCalls = append(m.dropCalls, name)
	if err := m.dropErrors[name]; err != nil {
		return err
	}
	if m.dropError != nil {
		return m.dropError
	}
	delete(m.databases, name)
	return nil
}

func (m *MockDatabaseClient) ListDatabases(pattern string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listCalls = append(m.listCalls, pattern)
	if err := m.listErrors[pattern]; err != nil {
		return nil, err
	}
	if m.listError != nil {
		return nil, m.listError
	}
	if scripted, ok := m.listResults[pattern]; ok {
		return append([]string(nil), scripted...), nil
	}

	result := make([]string, 0)
	for name := range m.databases {
		if likePatternMatches(pattern, name) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, nil
}

func likePatternMatches(pattern, value string) bool {
	var expression strings.Builder
	expression.WriteString("^")
	escaped := false
	for _, character := range pattern {
		if escaped {
			expression.WriteString(regexp.QuoteMeta(string(character)))
			escaped = false
			continue
		}
		switch character {
		case '\\':
			escaped = true
		case '%':
			expression.WriteString(".*")
		case '_':
			expression.WriteString(".")
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	if escaped {
		expression.WriteString(regexp.QuoteMeta(`\`))
	}
	expression.WriteString("$")
	return regexp.MustCompile(expression.String()).MatchString(value)
}

func (m *MockDatabaseClient) SetPingError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingError = err
}

func (m *MockDatabaseClient) SetCreateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createError = err
}

func (m *MockDatabaseClient) SetDropError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropError = err
}

func (m *MockDatabaseClient) SetDropErrorForDatabase(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropErrors[name] = err
}

func (m *MockDatabaseClient) SetListError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listError = err
}

func (m *MockDatabaseClient) SetListErrorForPattern(pattern string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listErrors[pattern] = err
}

func (m *MockDatabaseClient) SetListResultsForPattern(pattern string, names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listResults[pattern] = append([]string(nil), names...)
}

func (m *MockDatabaseClient) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeError = err
}

func (m *MockDatabaseClient) SetExistsOnFirstNCalls(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.existsOnCall = n
}

func (m *MockDatabaseClient) AddDatabase(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.databases[name] = true
}

func (m *MockDatabaseClient) GetCreateCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.createCalls...)
}

func (m *MockDatabaseClient) GetDropCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.dropCalls...)
}

func (m *MockDatabaseClient) GetListCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.listCalls...)
}

func (m *MockDatabaseClient) HasDatabase(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.databases[name]
}

func (m *MockDatabaseClient) DatabaseCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.databases)
}

func MockClientFactory(client *MockDatabaseClient) DatabaseClientFactory {
	return func(engine string, opts DatabaseOptions) (DatabaseClient, error) {
		return client, nil
	}
}

type MockClientFactoryRecorder struct {
	mu     sync.Mutex
	client DatabaseClient
	calls  int
	err    error
}

func NewMockClientFactoryRecorder(client DatabaseClient) *MockClientFactoryRecorder {
	return &MockClientFactoryRecorder{client: client}
}

func (r *MockClientFactoryRecorder) Factory(engine string, opts DatabaseOptions) (DatabaseClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.client, nil
}

func (r *MockClientFactoryRecorder) CallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *MockClientFactoryRecorder) SetError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}
