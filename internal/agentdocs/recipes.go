package agentdocs

// Recipe is one recipe of the embedded agent skill (docs/ARRANQUE.md §6.4):
// a task an AI agent resolves with one exact trafico command. Command is
// the exact argv, "trafico" included, so the skills command can print it
// verbatim and the tests can validate it against the live cobra command
// tree: a recipe that names a flag a later rename removed must fail the
// tests, not send an agent into an exit 2 with no idea why.
//
// Date placeholders appear as angle-bracket tokens (for example <sab>), the
// same convention docs/ARRANQUE.md §6.4 uses; the reading field tells the
// agent what to substitute.
type Recipe struct {
	// ID is the stable identifier the skills command takes as its argument.
	ID string `json:"id"`
	// Summary is the one-line purpose shown by `trafico skills`.
	Summary string `json:"summary"`
	// Command is the exact command line, one shell word per element,
	// starting with "trafico".
	Command []string `json:"command"`
	// Answers states what the command answers, so the agent can pick the
	// recipe without executing it.
	Answers string `json:"answers"`
	// Reading tells the agent how to read the output: the keys that
	// matter, the null semantics and what an empty list means.
	Reading string `json:"reading"`
}

// recipes are the four recipes of docs/ARRANQUE.md §6.4, embedded as data
// (ODD ola-5-agentes T1). User-facing text is Spanish, like the rest of
// the CLI; identifiers are English.
var recipes = []Recipe{
	{
		ID:      "estado-carretera",
		Summary: "Estado de una carretera por punto kilométrico",
		Command: []string{"trafico", "incidents", "--road", "A-7", "--pk", "100-140"},
		Answers: "Qué incidencias activas hay ahora en la A-7 entre los pk 100 y 140.",
		Reading: "Cada entrada de incidents lleva road, pk, type, severity y status. Con --json, mira meta.published_at y meta.stale antes de dar los datos por actuales; si meta.coverage_note aparece, la cobertura no incluye todo (Cataluña y País Vasco dependen de otras administraciones). Una lista vacía no es un error: la DGT no publica incidencias en ese tramo ahora mismo.",
	},
	{
		ID:      "mi-ruta",
		Summary: "Incidencias ahora en una ruta guardada",
		Command: []string{"trafico", "route", "check", "trabajo"},
		Answers: "Las incidencias activas en cada tramo de la ruta guardada «trabajo», en el orden en que se declaró.",
		Reading: "Cada elemento de segments corresponde a un tramo (road y pk) y trae sus incidents; un tramo sin incidencias sale con la lista vacía. La ruta se guarda antes con: trafico route add trabajo --segment A-7:100-140.",
	},
	{
		ID:      "obras-fin-de-semana",
		Summary: "Obras planificadas para el fin de semana",
		Command: []string{"trafico", "incidents", "--road", "N-332", "--type", "roadworks", "--status", "planned", "--from", "<sab>", "--to", "<dom>"},
		Answers: "Las obras (roadworks) aún no activas en la N-332 cuyo inicio cae entre el sábado <sab> y el domingo <dom>.",
		Reading: "Sustituye <sab> y <dom> por las fechas reales del fin de semana en formato YYYY-MM-DD; una fecha suelta incluye todo el día. planned solo trae lo que aún no ha empezado; con --status all verías también las activas.",
	},
	{
		ID:      "paneles",
		Summary: "Mensajes actuales de los paneles de una carretera",
		Command: []string{"trafico", "signs", "--road", "AP-7", "--direction", "positive"},
		Answers: "Los mensajes que muestran ahora los paneles de mensaje variable de la AP-7 en dirección positiva (kilómetros crecientes).",
		Reading: "message trae el texto del panel tal cual lo publica la DGT; road, direction y pk sitúan el panel. Una lista vacía no es un error: la DGT no publica ahora paneles para esa carretera y dirección.",
	},
}

// Recipes returns every embedded recipe, in declaration order. The slice is
// a copy: callers cannot mutate the embedded data.
func Recipes() []Recipe {
	return append([]Recipe(nil), recipes...)
}

// Get returns the recipe with the given id, and whether it exists.
func Get(id string) (Recipe, bool) {
	for _, recipe := range recipes {
		if recipe.ID == id {
			return recipe, true
		}
	}
	return Recipe{}, false
}
