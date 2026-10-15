import type { Shape } from "../shape";

export const en = {
  input: {
    stopped: "The server is stopped",
    placeholder: "Type / to see the commands",
    complete: "complete",
    send: "send",
    sendButton: "Send",
  },
  completion: {
    value: "value",
    noArgument: "no argument",
    defaultValue: "· default: {value}",
    flagOnly: "· option without value",
    closeQuotes: "Close the quotes to end the text (the server does not accept a quote inside).",
    useQuotes: "Put the text in quotes if it contains spaces: \"my text\".",
    variants: "+{n} variant(s)",
    syntax: "Syntax",
    onlinePlayer: "online player",
    quotedText: "quoted text",
    quotedTextHint: "For a text containing spaces",
    defaultHint: "default value",
  },
  refreshStarted: "Command analysis started",
};

export const fr: Shape<typeof en> = {
  input: {
    stopped: "Le serveur est arrêté",
    placeholder: "Tapez / pour voir les commandes",
    complete: "compléter",
    send: "envoyer",
    sendButton: "Envoyer",
  },
  completion: {
    value: "valeur",
    noArgument: "sans argument",
    defaultValue: "· défaut : {value}",
    flagOnly: "· option sans valeur",
    closeQuotes: "Fermez les guillemets pour terminer le texte (le serveur n'accepte pas de guillemet à l'intérieur).",
    useQuotes: "Entourez le texte de guillemets s'il contient des espaces : \"mon texte\".",
    variants: "+{n} variante(s)",
    syntax: "Syntaxe",
    onlinePlayer: "joueur connecté",
    quotedText: "texte entre guillemets",
    quotedTextHint: "Pour un texte contenant des espaces",
    defaultHint: "valeur par défaut",
  },
  refreshStarted: "Analyse des commandes lancée",
};
