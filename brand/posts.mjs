/**
 * The eight approved visual posts: five educational, three marketing.
 *
 * Content is data, not markup. Every string here is either lifted verbatim
 * from shipped copy (`website/index.html`, `website/app.js`) or condensed from
 * an authoritative engineering document, and every post records `sources` so a
 * reviewer can trace a claim back to the artefact that supports it. That
 * traceability is required by the integration document, section 16.
 *
 * Every string passes `claims.mjs`. Adding a post that breaks a claim rule
 * fails the render and the test suite.
 */

/**
 * @typedef {object} Card
 * @property {string} label Short badge, either an index like `01` or a word like `Managed`.
 * @property {string} title Card heading.
 * @property {string} body Card copy.
 * @property {string} note Footer tag on the card.
 */

/**
 * @typedef {object} Row
 * @property {string} label Two-digit index.
 * @property {string} title Layer name.
 * @property {string} body One-sentence description.
 * @property {string[]} points Supporting detail, used by the carousel only.
 */

/**
 * @typedef {object} Panel
 * @property {"positive" | "negative"} tone Drives the accent treatment.
 * @property {string} label Column heading.
 * @property {string} title Column subject.
 * @property {string[]} items Bulleted detail.
 */

/**
 * @typedef {object} Post
 * @property {string} id Filename stem and finding label.
 * @property {"educational" | "marketing"} kind Editorial category.
 * @property {string} tag Badge printed top-right on every canvas.
 * @property {"statement" | "cards" | "rows" | "split"} template Layout family.
 * @property {string} eyebrow Small uppercase kicker.
 * @property {string} headline The one thing the post says.
 * @property {string} lede Supporting sentence. Dropped on `wide` canvases for dense templates.
 * @property {string[]} sources Repository paths that support the copy.
 * @property {Card[]} [cards] Required by the `cards` template.
 * @property {Row[]} [rows] Required by the `rows` template.
 * @property {Panel[]} [panels] Required by the `split` template, exactly two.
 * @property {Array<{label: string, title: string, body: string}>} [points] Optional numbered steps for `statement`.
 * @property {string[]} [strip] Optional audience strip for `statement`.
 * @property {boolean} [carousel] Render an additional portrait slide sequence.
 */

/** @type {ReadonlyArray<Post>} */
export const POSTS = Object.freeze([
  Object.freeze({
    id: "e1-security-gap",
    kind: "educational",
    tag: "Explainer",
    template: "cards",
    eyebrow: "The security gap",
    headline: "Encryption alone does not hide *the whole story.*",
    lede: "Modern adversaries target long-lived cryptography, traffic metadata and centralised identity systems. Haechi is designed around all three pressure points.",
    sources: ["website/index.html, threat section"],
    cards: [
      {
        label: "01",
        title: "Quantum transition",
        body: "Long-duration sensitive data needs protection before cryptographically relevant quantum systems arrive, not after the fact.",
        note: "Harvest-now risk",
      },
      {
        label: "02",
        title: "Metadata exposure",
        body: "Timing, volume, routes and protocol fingerprints can reveal structure and intent even when an attacker cannot read the payload.",
        note: "Traffic analysis",
      },
      {
        label: "03",
        title: "Centralised trust",
        body: "Identity-linked sessions and broad provider logs create high-value correlation points for compromise, coercion and insider risk.",
        note: "Correlation risk",
      },
    ],
  }),

  Object.freeze({
    id: "e2-five-layers",
    kind: "educational",
    tag: "Architecture",
    template: "rows",
    carousel: true,
    eyebrow: "System architecture",
    headline: "Five layers. *Explicit trust boundaries.*",
    lede: "How a connection moves from enterprise identity to protected egress without collapsing every responsibility into one privileged service.",
    sources: [
      "website/app.js, architecture map",
      "dev-docs/architecture/overview.md",
    ],
    rows: [
      {
        label: "01",
        title: "Endpoint",
        body: "A policy-controlled agent is the only component that coordinates tunnel state on a device, holding no ambient privilege of its own.",
        points: [
          "Hardware-backed device keys where supported",
          "Windows, macOS, Linux, Android and iOS",
          "Fail-closed networking with rapid rollback",
        ],
      },
      {
        label: "02",
        title: "Privacy-separated access",
        body: "Enterprise identity remains with the identity provider. The endpoint requests a short-lived, bounded capability instead.",
        points: [
          "Short-lived and audience-bound capabilities",
          "Replay protection and explicit policy epochs",
          "Independent cryptographic and privacy review gate",
        ],
      },
      {
        label: "03",
        title: "Control plane",
        body: "One governed source of configuration truth: tenancy, device state, policy, gateway inventory and signed route plans.",
        points: [
          "Federated identity and deny-by-default authorisation",
          "Versioned contracts and signed policy bundles",
          "High-availability state and auditable change control",
        ],
      },
      {
        label: "04",
        title: "Regional data plane",
        body: "Gateways verify a scoped capability locally and establish a reinforced tunnel. They never receive the identity-provider session.",
        points: [
          "Admission decided at the gateway with no identity call",
          "Direct mode and isolated protected transports",
          "Capacity, abuse and regional policy controls",
        ],
      },
      {
        label: "05",
        title: "Global Intelligence Mesh",
        body: "Privacy-filtered operational features inform gateway scoring and shaping policy. Models cannot deploy directly.",
        points: [
          "No payloads, raw DNS activity or durable user IDs",
          "Signed, compatible and rollback-safe policies",
          "Customer-controlled telemetry for dedicated deployments",
        ],
      },
    ],
  }),

  Object.freeze({
    id: "e3-no-silent-downgrade",
    kind: "educational",
    tag: "Explainer",
    template: "split",
    eyebrow: "Post-quantum transition",
    headline: "*Hybrid,* never replacement.",
    lede: "A proven data plane stays in place while a post-quantum key exchange continuously refreshes the keys protecting it.",
    sources: [
      "dev-docs/adr/adr-005-pq-reinforcement.md",
      "website/index.html, pillar 01",
    ],
    panels: [
      {
        tone: "positive",
        label: "What the design adds",
        title: "Reinforcement",
        items: [
          "A post-quantum key exchange refreshes the tunnel key material",
          "Control-plane cryptography follows standardised, interoperable algorithms",
          "Strength is the stronger of the two paths, never weaker than the classical one",
        ],
      },
      {
        tone: "negative",
        label: "What never happens",
        title: "Silent downgrade",
        items: [
          "A required reinforcement that cannot be negotiated fails the connection",
          "No quiet fallback to the weaker path when negotiation fails",
          "No unrecorded change of cryptographic posture",
        ],
      },
    ],
  }),

  Object.freeze({
    id: "e4-privacy-separated-access",
    kind: "educational",
    tag: "Explainer",
    template: "split",
    eyebrow: "Privacy-separated access",
    headline: "Entitlement in. *Durable identity out.*",
    lede: "Regional gateways are designed to admit an authorised session without retaining a durable user identity.",
    sources: [
      "dev-docs/adr/adr-004-capability-trust-model.md",
      "website/app.js, layer 02",
    ],
    panels: [
      {
        tone: "positive",
        label: "What the gateway receives",
        title: "A scoped capability",
        items: [
          "Short-lived, audience-bound and epoch-limited",
          "A coarse gateway class and service class, never a destination",
          "Replay protection with explicit policy epochs",
        ],
      },
      {
        tone: "negative",
        label: "What the gateway never receives",
        title: "Durable identity",
        items: [
          "The identity-provider session",
          "A durable user identifier, email address or account id",
          "The destination behind the request",
        ],
      },
    ],
  }),

  Object.freeze({
    id: "e5-telemetry-boundary",
    kind: "educational",
    tag: "Data contract",
    template: "split",
    eyebrow: "Telemetry boundary",
    headline: "*Prohibited fields* cannot cross.",
    lede: "The boundary drops any event containing a prohibited field and records only that a drop occurred, never the prohibited value.",
    sources: ["dev-docs/threat-model/telemetry-prohibition-list.md"],
    panels: [
      {
        tone: "negative",
        label: "Must never cross",
        title: "Dropped at the boundary",
        items: [
          "Traffic payload and request bodies",
          "DNS query content and resolved answers",
          "Durable user identifiers and identity assertions",
          "Raw client addresses, capability tokens, keys and nonces",
        ],
      },
      {
        tone: "positive",
        label: "Permitted",
        title: "Coarse operational signals",
        items: [
          "gateway_id, region and service_class",
          "pq_mode: hybrid-pq or classical",
          "outcome: success, failure or denied",
          "latency_bucket, epoch and pre-aggregated counters",
        ],
      },
    ],
  }),

  Object.freeze({
    id: "m1-reveal-less",
    kind: "marketing",
    tag: "Platform",
    template: "statement",
    eyebrow: "Secure communications for the quantum era",
    headline: "Communications that *reveal less.*",
    lede: "An enterprise network platform that separates identity from connection activity, reinforces tunnels for the post-quantum transition and adapts safely to hostile networks.",
    sources: ["website/index.html, hero"],
    strip: [
      "Enterprise",
      "Government contractors",
      "Critical infrastructure",
      "High-assurance teams",
    ],
  }),

  Object.freeze({
    id: "m2-operating-models",
    kind: "marketing",
    tag: "Deployment",
    template: "cards",
    eyebrow: "Deployment flexibility",
    headline: "One architecture, *three operating models.*",
    lede: "The same trust boundaries, placed wherever the environment requires them.",
    sources: ["website/index.html, deployment section"],
    cards: [
      {
        label: "Managed",
        title: "Multi-tenant SaaS",
        body: "Regional service with tested tenant isolation, managed upgrades and dedicated gateway pools where required.",
        note: "Fastest route to pilot",
      },
      {
        label: "Dedicated",
        title: "Enterprise and CNI",
        body: "A single-tenant control and data plane in a customer-approved account or region, with customer-controlled keys.",
        note: "Customer-managed trust roots",
      },
      {
        label: "Restricted",
        title: "Disconnected network",
        body: "Self-contained operation with signed offline release, policy and intelligence promotion workflows.",
        note: "No public SaaS dependency",
      },
    ],
  }),

  Object.freeze({
    id: "m3-enterprise-pilot",
    kind: "marketing",
    tag: "Enterprise pilot",
    template: "statement",
    eyebrow: "Assurance before adjectives",
    headline: "Define the environment. *Test the evidence.*",
    lede: "Tell us what you need to protect, where it must run and which assurance requirements matter. We will shape a controlled pilot around a documented threat model.",
    sources: ["website/index.html, pilot and assurance sections"],
    points: [
      {
        label: "01",
        title: "Discovery",
        body: "Architecture and threat-model workshop.",
      },
      {
        label: "02",
        title: "Deployment",
        body: "Bounded pilot in an approved environment.",
      },
      {
        label: "03",
        title: "Evidence",
        body: "Measured results and an explicit risk register.",
      },
    ],
  }),
]);

/** @type {ReadonlyArray<string>} */
export const POST_IDS = Object.freeze(POSTS.map((p) => p.id));

/**
 * Look up a post by id.
 *
 * @param {string} id A member of {@link POST_IDS}.
 * @returns {Post}
 * @throws {Error} If no post has that id.
 */
export function postById(id) {
  const found = POSTS.find((p) => p.id === id);
  if (found === undefined) {
    throw new Error(
      `unknown post '${id}', expected one of ${POST_IDS.join(", ")}`,
    );
  }
  return found;
}
