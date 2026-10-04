package notifier

import (
	"context"
	"strings"

	"go-trade-bot/internal/i18n"
)

// Messages is the notification catalog (i18n-02 §4): the static words of
// the webhook formatters, the Go-authored agent notifications and the
// worker's trade-event texts. EN is the source of truth (a test fails when
// ES or PT-BR lacks a key). Everything is sent in Settings.DefaultLocale;
// model-written text is sent as is.
var Messages = i18n.Catalog{
	i18n.EN: {
		"severity.info":     "INFO",
		"severity.warning":  "WARNING",
		"severity.critical": "CRITICAL",
		"link.report":       "Open report",
		"link.proposal":     "Open proposal",
		"footer.agent":      "Agent %s",

		"budget.title":   "Daily budget exhausted",
		"budget.message": "Agent %q has spent $%.2f of its $%.2f daily budget (UTC day). It will not run again until tomorrow unless the budget is raised.",
		"test.title":     "Test notification",
		"test.message":   "This is a test message for webhook target %q.",

		"proposal.promotion.title":    "Promotion proposal #%d: %s",
		"proposal.promotion.message":  "Agent %q proposes promoting challenger #%d into %q (gate passed: %s). Review and approve or reject in the web UI.",
		"proposal.promotion.early":    "%s, EARLY",
		"bool.true":                   "yes",
		"bool.false":                  "no",
		"proposal.label.proposal":     "Proposal",
		"proposal.label.promotion":    "Promotion",
		"proposal.applied.title":      "%s #%d applied: %s",
		"proposal.applied.message":    "The approved change to %q is now live in its code (mode %s, status %s unchanged).",
		"proposal.superseded.title":   "Proposal #%d superseded",
		"proposal.superseded.message": "Proposal #%d for %q was not applied: the target strategy's code changed after approval.",
		"proposal.failed.title":       "Proposal #%d failed",
		"proposal.failed.message":     "Proposal #%d was not applied: %s.",

		"event.position_opened":          "Position opened for %s",
		"event.position_closed":          "Position closed for %s",
		"event.position_closed_stop":     "Position closed for %s via exchange stop-loss (reconciled on race)",
		"event.position_closed_sim_stop": "Position closed for %s via simulated stop-loss (dryrun)",
	},
	i18n.ES: {
		"severity.info":     "INFO",
		"severity.warning":  "ADVERTENCIA",
		"severity.critical": "CRÍTICO",
		"link.report":       "Abrir reporte",
		"link.proposal":     "Abrir propuesta",
		"footer.agent":      "Agente %s",

		"budget.title":   "Presupuesto diario agotado",
		"budget.message": "El agente %q gastó $%.2f de su presupuesto diario de $%.2f (día UTC). No volverá a ejecutarse hasta mañana, salvo que se aumente el presupuesto.",
		"test.title":     "Notificación de prueba",
		"test.message":   "Este es un mensaje de prueba para el destino de webhook %q.",

		"proposal.promotion.title":    "Propuesta de promoción #%d: %s",
		"proposal.promotion.message":  "El agente %q propone promover el challenger #%d a %q (gate aprobado: %s). Revísala y apruébala o recházala en la app web.",
		"proposal.promotion.early":    "%s, ANTICIPADA",
		"bool.true":                   "sí",
		"bool.false":                  "no",
		"proposal.label.proposal":     "Propuesta",
		"proposal.label.promotion":    "Promoción",
		"proposal.applied.title":      "%s #%d aplicada: %s",
		"proposal.applied.message":    "El cambio aprobado a %q ya está activo en su código (modo %s y estado %s sin cambios).",
		"proposal.superseded.title":   "Propuesta #%d reemplazada",
		"proposal.superseded.message": "La propuesta #%d para %q no se aplicó: el código de la estrategia cambió después de la aprobación.",
		"proposal.failed.title":       "Propuesta #%d fallida",
		"proposal.failed.message":     "La propuesta #%d no se aplicó: %s.",

		"event.position_opened":          "Posición abierta en %s",
		"event.position_closed":          "Posición cerrada en %s",
		"event.position_closed_stop":     "Posición cerrada en %s por el stop-loss del exchange (conciliada tras una carrera)",
		"event.position_closed_sim_stop": "Posición cerrada en %s por el stop-loss simulado (dryrun)",
	},
	i18n.PTBR: {
		"severity.info":     "INFO",
		"severity.warning":  "ALERTA",
		"severity.critical": "CRÍTICO",
		"link.report":       "Abrir relatório",
		"link.proposal":     "Abrir proposta",
		"footer.agent":      "Agente %s",

		"budget.title":   "Orçamento diário esgotado",
		"budget.message": "O agente %q gastou $%.2f do orçamento diário de $%.2f (dia UTC). Ele só voltará a rodar amanhã, a menos que o orçamento seja aumentado.",
		"test.title":     "Notificação de teste",
		"test.message":   "Esta é uma mensagem de teste para o destino de webhook %q.",

		"proposal.promotion.title":    "Proposta de promoção #%d: %s",
		"proposal.promotion.message":  "O agente %q propõe promover o challenger #%d para %q (gate aprovado: %s). Revise e aprove ou rejeite no app web.",
		"proposal.promotion.early":    "%s, ANTECIPADA",
		"bool.true":                   "sim",
		"bool.false":                  "não",
		"proposal.label.proposal":     "Proposta",
		"proposal.label.promotion":    "Promoção",
		"proposal.applied.title":      "%s #%d aplicada: %s",
		"proposal.applied.message":    "A alteração aprovada em %q já está ativa no código (modo %s e status %s inalterados).",
		"proposal.superseded.title":   "Proposta #%d substituída",
		"proposal.superseded.message": "A proposta #%d para %q não foi aplicada: o código da estratégia mudou depois da aprovação.",
		"proposal.failed.title":       "Proposta #%d falhou",
		"proposal.failed.message":     "A proposta #%d não foi aplicada: %s.",

		"event.position_opened":          "Posição aberta em %s",
		"event.position_closed":          "Posição encerrada em %s",
		"event.position_closed_stop":     "Posição encerrada em %s pelo stop-loss da exchange (conciliada após disputa)",
		"event.position_closed_sim_stop": "Posição encerrada em %s pelo stop-loss simulado (dryrun)",
	},
}

// Text is a catalog message plus its fmt args. An AgentMessage carrying
// TitleText/MessageText is re-rendered in Settings.DefaultLocale at send time.
// Args must be locale-neutral (numbers, names, ids) - or Text values, which
// are rendered in the same locale first.
type Text struct {
	Key  string
	Args []any
}

// T builds a Text.
func T(key string, args ...any) *Text { return &Text{Key: key, Args: args} }

// Render renders t in loc (nested *Text args included).
func (t *Text) Render(loc i18n.Locale) string {
	if t == nil {
		return ""
	}
	args := make([]any, len(t.Args))
	for i, a := range t.Args {
		if nested, ok := a.(*Text); ok {
			args[i] = nested.Render(loc)
		} else {
			args[i] = a
		}
	}
	return Messages.F(loc, t.Key, args...)
}

// Bool is a localized yes/no Text.
func Bool(v bool) *Text {
	if v {
		return T("bool.true")
	}
	return T("bool.false")
}

// WithText sets Title/Message to their EN rendering and keeps the keys so
// the notifier can re-render them in Settings.DefaultLocale at send time.
func (m AgentMessage) WithText(title, message *Text) AgentMessage {
	if title != nil {
		m.TitleText, m.Title = title, title.Render(i18n.EN)
	}
	if message != nil {
		m.MessageText, m.Message = message, message.Render(i18n.EN)
	}
	return m
}

// localize fixes m's locale and re-renders catalog-backed text in it.
func (m AgentMessage) localize(loc i18n.Locale) AgentMessage {
	m.Locale = loc
	if m.TitleText != nil {
		m.Title = m.TitleText.Render(loc)
	}
	if m.MessageText != nil {
		m.Message = m.MessageText.Render(loc)
	}
	return m
}

func sourceLocale(ctx context.Context, src i18n.Source) i18n.Locale {
	if src == nil {
		return i18n.Default
	}
	return src.DefaultLocale(ctx)
}

// eventMessageKeys are the worker's trade-event texts, matched against the
// exact EN message the emitter produced (the emitters stay untouched).
var eventMessageKeys = map[EventType][]string{
	EventPositionOpened: {"event.position_opened"},
	EventPositionClosed: {"event.position_closed", "event.position_closed_stop", "event.position_closed_sim_stop"},
}

// LocalizeEvent translates a known trade-event message into loc. Unknown or
// dynamic messages (e.g. error details) are returned unchanged.
func LocalizeEvent(e Event, loc i18n.Locale) Event {
	if loc == i18n.EN {
		return e
	}
	for _, key := range eventMessageKeys[e.Type] {
		if e.Message == Messages.F(i18n.EN, key, e.Symbol) {
			e.Message = Messages.F(loc, key, e.Symbol)
			return e
		}
	}
	return e
}

func linkLabel(loc i18n.Locale, link string) string {
	if strings.Contains(link, "/agents/proposals/") {
		return Messages.T(loc, "link.proposal")
	}
	return Messages.T(loc, "link.report")
}
