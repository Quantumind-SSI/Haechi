document.documentElement.classList.add("js");

const header = document.querySelector("[data-header]");
const menuToggle = document.querySelector("[data-menu-toggle]");
const navigation = document.querySelector("[data-navigation]");

function closeMenu() {
  if (!menuToggle || !navigation) return;
  menuToggle.setAttribute("aria-expanded", "false");
  navigation.classList.remove("open");
  document.body.classList.remove("menu-open");
}

menuToggle?.addEventListener("click", () => {
  const willOpen = menuToggle.getAttribute("aria-expanded") !== "true";
  menuToggle.setAttribute("aria-expanded", String(willOpen));
  navigation?.classList.toggle("open", willOpen);
  document.body.classList.toggle("menu-open", willOpen);
});

navigation
  ?.querySelectorAll("a")
  .forEach((link) => link.addEventListener("click", closeMenu));
window.addEventListener("resize", () => {
  if (window.innerWidth > 780) closeMenu();
});

function updateHeader() {
  header?.classList.toggle("scrolled", window.scrollY > 24);
}

updateHeader();
window.addEventListener("scroll", updateHeader, { passive: true });

const architecture = {
  endpoint: {
    label: "Layer 01 · Endpoint",
    title: "A policy-controlled agent on every device.",
    copy: "The Haechi agent is the only component that coordinates tunnel state on a device. It sequences enrolment, key reinforcement, DNS protection, kill-switch enforcement and signed updates behind native platform adapters, holding no ambient privilege of its own.",
    points: [
      "Hardware-backed device keys where supported",
      "Windows, macOS, Linux, Android and iOS",
      "Fail-closed networking with rapid rollback",
    ],
  },
  capability: {
    label: "Layer 02 · Privacy-separated access",
    title: "Entitlement in. Durable identity out.",
    copy: "Enterprise identity remains with the identity provider. The endpoint requests a short-lived, bounded capability designed to be presented to a gateway without revealing the user’s durable identity.",
    points: [
      "Short-lived and audience-bound capabilities",
      "Replay protection and explicit policy epochs",
      "Independent cryptographic and privacy review gate",
    ],
  },
  control: {
    label: "Layer 03 · Control plane",
    title: "One governed source of configuration truth.",
    copy: "Haechi’s control plane manages tenancy, device state, policy, gateway inventory and signed route plans while keeping identity responsibilities separate from connection admission.",
    points: [
      "Federated enterprise identity and deny-by-default authorisation",
      "Versioned service contracts and signed policy bundles",
      "High-availability state and auditable change control",
    ],
  },
  gateway: {
    label: "Layer 04 · Regional data plane",
    title: "Fast encrypted transit with minimal knowledge.",
    copy: "Regional gateways verify a scoped capability locally, establish a reinforced encrypted tunnel and apply controlled egress policy. They never receive the user’s identity-provider session, so a compromised gateway cannot reconstruct who its traffic belongs to.",
    points: [
      "Admission decided at the gateway with no identity call",
      "Direct mode and isolated protected transports",
      "Capacity, abuse and regional policy controls",
    ],
  },
  intelligence: {
    label: "Layer 05 · Global Intelligence Mesh",
    title: "Adaptive policy without a surveillance dataset.",
    copy: "Privacy-filtered operational features inform gateway scoring, transport recommendations and shaping policy. Models cannot deploy directly: every release is evaluated, signed, constrained and reversible.",
    points: [
      "No payloads, raw DNS activity or durable user IDs",
      "Signed, compatible and rollback-safe policies",
      "Customer-controlled telemetry for dedicated deployments",
    ],
  },
};

const architectureTabs = [...document.querySelectorAll("[data-stage]")];
const architectureLabel = document.querySelector("[data-architecture-label]");
const architectureTitle = document.querySelector("[data-architecture-title]");
const architectureCopy = document.querySelector("[data-architecture-copy]");
const architecturePoints = document.querySelector("[data-architecture-points]");
const layerLabels = [...document.querySelectorAll(".layer-map span")];

function selectArchitecture(stageName) {
  const selected = architecture[stageName];
  if (!selected) return;

  architectureTabs.forEach((tab) =>
    tab.setAttribute("aria-selected", String(tab.dataset.stage === stageName)),
  );
  if (architectureLabel) architectureLabel.textContent = selected.label;
  if (architectureTitle) architectureTitle.textContent = selected.title;
  if (architectureCopy) architectureCopy.textContent = selected.copy;
  if (architecturePoints) {
    architecturePoints.replaceChildren(
      ...selected.points.map((point) => {
        const item = document.createElement("li");
        item.textContent = point;
        return item;
      }),
    );
  }

  const activeIndex = architectureTabs.findIndex(
    (tab) => tab.dataset.stage === stageName,
  );
  layerLabels.forEach((label, index) =>
    label.classList.toggle("active", index === activeIndex),
  );
}

architectureTabs.forEach((tab, index) => {
  tab.addEventListener("click", () => selectArchitecture(tab.dataset.stage));
  tab.addEventListener("keydown", (event) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    let nextIndex = index;
    if (event.key === "ArrowLeft")
      nextIndex =
        (index - 1 + architectureTabs.length) % architectureTabs.length;
    if (event.key === "ArrowRight")
      nextIndex = (index + 1) % architectureTabs.length;
    if (event.key === "Home") nextIndex = 0;
    if (event.key === "End") nextIndex = architectureTabs.length - 1;
    architectureTabs[nextIndex].focus();
    selectArchitecture(architectureTabs[nextIndex].dataset.stage);
  });
});

const revealElements = document.querySelectorAll(".reveal");
if ("IntersectionObserver" in window) {
  const observer = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        entry.target.classList.add("visible");
        observer.unobserve(entry.target);
      });
    },
    { threshold: 0.12, rootMargin: "0px 0px -40px" },
  );
  revealElements.forEach((element) => observer.observe(element));
} else {
  revealElements.forEach((element) => element.classList.add("visible"));
}

const pilotForm = document.querySelector("[data-pilot-form]");
const formStatus = document.querySelector("[data-form-status]");

function clearFormErrors() {
  pilotForm
    ?.querySelectorAll('[aria-invalid="true"]')
    .forEach((field) => field.removeAttribute("aria-invalid"));
  pilotForm?.querySelectorAll("[data-error-for]").forEach((error) => {
    error.textContent = "";
  });
}

function showFieldErrors(fields = {}) {
  Object.entries(fields).forEach(([fieldName, message]) => {
    const field = pilotForm?.elements.namedItem(fieldName);
    if (field instanceof HTMLElement)
      field.setAttribute("aria-invalid", "true");
    const output = pilotForm?.querySelector(`[data-error-for="${fieldName}"]`);
    if (output) output.textContent = message;
  });
}

pilotForm?.addEventListener("submit", async (event) => {
  event.preventDefault();
  clearFormErrors();

  const submitButton = pilotForm.querySelector('button[type="submit"]');
  const formData = new FormData(pilotForm);
  const payload = {
    organisation: formData.get("organisation"),
    workEmail: formData.get("workEmail"),
    role: formData.get("role"),
    deployment: formData.get("deployment"),
    message: formData.get("message"),
    website: formData.get("website"),
    consent: formData.get("consent") === "on",
  };

  if (submitButton) submitButton.disabled = true;
  if (formStatus) {
    formStatus.classList.remove("error");
    formStatus.textContent = "Submitting securely…";
  }

  try {
    // Cloudflare serves these assets; the API lives in Azure. config.js is
    // generated at build time and is absent when both are same-origin.
    const apiBase = window.HAECHI_CONFIG?.apiBase || "";
    const response = await fetch(`${apiBase}/api/pilot`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const result = await response.json();

    if (!response.ok) {
      showFieldErrors(result.fields);
      throw new Error(result.error || "Unable to submit your request.");
    }

    pilotForm.reset();
    if (formStatus)
      formStatus.textContent = `Request received. Your reference is ${result.reference}.`;
  } catch (error) {
    if (formStatus) {
      formStatus.classList.add("error");
      formStatus.textContent =
        error.message || "Unable to submit your request.";
    }
  } finally {
    if (submitButton) submitButton.disabled = false;
  }
});

document.querySelectorAll("[data-year]").forEach((element) => {
  element.textContent = String(new Date().getFullYear());
});
