package config

// Built-in templates; always available, overridable by id from the user config's "layouts".
var BuiltinLayouts = []Layout{
	{ID: "balanced", Name: "Balanced", Columns: []string{"868px", "600px", "1fr"}, Slots: []string{"ai", "health", "launcher"}},
	{ID: "agents", Name: "Agents", Columns: []string{"1240px", "228px", "1fr"}, Slots: []string{"ai", "health-mini", "launcher"}},
	{ID: "focus", Name: "Focus", Columns: []string{"1240px", "228px", "1fr"}, Slots: []string{"ai", "health-mini", "launcher"}, Focus: true},
	{ID: "ai-desk", Name: "AI desk", Columns: []string{"868px", "600px", "1fr"}, Slots: []string{"ai", "limits", "launcher"}, Requires: "redline"},
	{ID: "ops-desk", Name: "Ops desk", Columns: []string{"868px", "600px", "1fr"}, Slots: []string{"ai", "ops", "launcher"}, Requires: "ops"},
	{ID: "usage", Name: "AI usage", Columns: []string{"600px", "1fr"}, Slots: []string{"limits", "usage"}},
	{ID: "system", Name: "System", Columns: []string{"1474px", "1fr"}, Slots: []string{"system", "launcher"}},
	{ID: "monitor", Name: "Monitor", Columns: []string{"800px", "1fr"}, Slots: []string{"health", "system"}},
}

// ForIntegrations keeps the layouts whose integration is present and drops widget slots (and their
// columns) whose integration is absent, so templates never show an empty or "not installed" block.
func ForIntegrations(layouts []Layout, have map[string]bool) []Layout {
	out := make([]Layout, 0, len(layouts))
	for _, l := range layouts {
		if l.Requires != "" && !have[l.Requires] {
			continue
		}
		var cols, slots []string
		for i, w := range l.Slots {
			if r := WidgetRequires[w]; r != "" && !have[r] {
				continue
			}
			cols, slots = append(cols, l.Columns[i]), append(slots, w)
		}
		if len(slots) == 0 {
			continue
		}
		l.Columns, l.Slots = cols, slots
		out = append(out, l)
	}
	return out
}

// AllLayouts returns built-ins with user overrides applied, followed by user-only layouts.
func (c *Config) AllLayouts() []Layout {
	user := map[string]Layout{}
	for _, l := range c.Layouts {
		user[l.ID] = l
	}
	out := make([]Layout, 0, len(BuiltinLayouts)+len(c.Layouts))
	for _, b := range BuiltinLayouts {
		if u, ok := user[b.ID]; ok {
			out = append(out, u)
			delete(user, b.ID)
		} else {
			out = append(out, b)
		}
	}
	for _, l := range c.Layouts {
		if _, ok := user[l.ID]; ok {
			out = append(out, l)
		}
	}
	return out
}

// PageOrder is the user's rotation and number-key order, filtered to layouts that exist.
func (c *Config) PageOrder() []string {
	known := map[string]bool{}
	for _, l := range c.AllLayouts() {
		known[l.ID] = true
	}
	order := c.UI.Layouts
	if len(order) == 0 {
		order = []string{"balanced", "ai-desk", "ops-desk", "agents", "usage", "system"}
	}
	out := []string{}
	for _, id := range order {
		if known[id] {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		out = []string{"balanced"}
	}
	return out
}
