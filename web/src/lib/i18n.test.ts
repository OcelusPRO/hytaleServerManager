import { describe, expect, it } from "vitest";
import { en, fr } from "../locales";
import { serverMessagesFr } from "../locales/serverMessages.fr";
import { translateServer } from "./i18n";

describe("translateServer", () => {
  it("leaves English untouched", () => {
    expect(translateServer("the server is already running", "en")).toBe("the server is already running");
  });

  it("translates exact messages", () => {
    expect(translateServer("the server is already running", "fr")).toBe("le serveur est déjà démarré");
  });

  it("translates the values inserted in a message", () => {
    expect(translateServer("Incoming connection (172.25.0.1:59128): waking the server up…", "fr")).toBe(
      "Connexion entrante (172.25.0.1:59128) : réveil du serveur…",
    );
    expect(translateServer("Automatic update (server empty for 10m0s): stopping the server…", "fr")).toBe(
      "Mise à jour automatique (serveur vide depuis 10m0s) : arrêt du serveur…",
    );
  });

  it("translates wrapped errors", () => {
    expect(translateServer("Hytale game session: no Hytale account is connected", "fr")).toBe(
      "session de jeu Hytale: aucun compte Hytale n'est connecté",
    );
  });

  it("translates each line of a multi-line message", () => {
    expect(translateServer("invalid port\ninvalid patchline", "fr")).toBe("port invalide\npatchline invalide");
  });

  it("shows unknown messages as they are", () => {
    expect(translateServer("something new happened", "fr")).toBe("something new happened");
  });
});

describe("catalogs", () => {
  it("keeps every placeholder of the server messages", () => {
    for (const [source, translation] of serverMessagesFr) {
      const placeholders = (s: string) => (s.match(/\{\d+\}/g) ?? []).sort().join();
      expect(placeholders(translation), source).toBe(placeholders(source));
    }
  });

  it("keeps the named parameters of the interface messages", () => {
    const walk = (a: unknown, b: unknown, path: string) => {
      if (typeof a === "string") {
        const params = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort().join();
        expect(params(b as string), path).toBe(params(a));
        return;
      }
      for (const key of Object.keys(a as object)) walk((a as never)[key], (b as never)[key], `${path}.${key}`);
    };
    walk(en, fr, "fr");
  });
});
