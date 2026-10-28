# Backlog de refactores

Trabajo identificado que **no** entra en la tarea que lo descubrió. `docs/ARRANQUE.md` §10.5
manda aquí lo que aparece en una revisión y no es corrección de un blocker: una tarea, un PR, y se
discute en su momento en vez de colarse en la ronda de corrección de otra.

| ID | Origen | Qué | Por qué no se hizo en su momento |
|---|---|---|---|
| REF-1 | CMD-02, revisión independiente (nit 9) | `internal/app/feeds.go` vuelve a leer la entrada de caché con `Cache.Load` justo después de que `Fetcher.Fetch` ya la leyó: para el feed de incidencias eso significa abrir y recorrer 5,8 MB solo para cerrar el cuerpo. Exponer el `Entry` en `Result` lo elimina. | Requiere tocar `internal/feeds`, que es superficie de una tarea ya cerrada (FEED-01). Mezclarlo en la corrección de CMD-02 habría reabierto una tarea mergeada. |

## Cómo se usa

- Un hallazgo de revisión que **no** es blocker de la tarea actual: se anota aquí con su `REF-<n>`,
  se referencia en el informe y se sigue adelante.
- Se cierra con su propio PR, con tests, cuando el usuario lo priorice.
