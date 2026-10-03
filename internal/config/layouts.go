package config

// Built-in templates; always available, overridable by id from the user config's "layouts".
var BuiltinLayouts = []Layout{
	{ID: "balanced", Name: "Balanced", Columns: []string{"868px", "600px", "1fr"}, Slots: []string{"ai", "health", "launcher"}},
	{ID: "agents", Name: "Agents", Columns: []string{"1240px", "228px", "1fr"}, Slots: []string{"ai", "health-mini", "launcher"}},
	{ID: "focus", Name: "Focus", Columns: []string{"1240px", "228px", "1fr"}, Slots: []string{"ai", "health-mini", "launcher"}, Focus: true},
	{ID: "usage", Name: "AI usage", Columns: []string{"1474px", "1fr"}, Slots: []string{"usage", "launcher"}},
	{ID: "system", Name: "System", Columns: []string{"1474px", "1fr"}, Slots: []string{"system", "launcher"}},
	{ID: "monitor", Name: "Monitor", Columns: []string{"800px", "1fr"}, Slots: []string{"health", "system"}},
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
		order = []string{"balanced", "agents", "usage", "system"}
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
