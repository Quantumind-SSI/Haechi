# Email Routing for haechi.co.uk

Status: specified, not yet enabled
Last verified: 2026-08-30

Cloudflare Email Routing receives mail for `haechi.co.uk` and forwards it to a
single verified destination. It is receive and forward only. Nothing can send
as `@haechi.co.uk` through it.

## Configuration to apply

| Address                 | Purpose                                            |
| ----------------------- | -------------------------------------------------- |
| `hello@haechi.co.uk`    | General enquiries, published on the website        |
| `admin@haechi.co.uk`    | Administrative and account correspondence          |
| `security@haechi.co.uk` | Vulnerability reports, referenced by `SECURITY.md` |
| `license@haechi.co.uk`  | Licensing enquiries, referenced by `LICENSE`       |

All four forward to `haechi.iss@gmail.com`.

No catch-all rule. A catch-all accepts mail to every possible local part, which
attracts directory-harvest spam and hides typos in published addresses. Add
specific addresses instead as they are needed.

## Why this is not automated

Enabling Email Routing requires a token with `Zone → Email Routing → Edit`. The
token currently held provides `Zone → DNS → Edit` only, and every Email Routing
endpoint returns HTTP 403:

```
GET  /zones/{zone}/email/routing            403
GET  /zones/{zone}/email/routing/dns        403
GET  /zones/{zone}/email/routing/rules      403
GET  /accounts/{account}/email/routing/addresses  403
POST /zones/{zone}/email/routing/enable     403
```

The MX hostnames are also not guessable. Cloudflare assigns them per zone, and
`/email/routing/dns` is the endpoint that reveals them. Publishing an incorrect
MX record does not fail loudly; it silently loses inbound mail. So the records
are created by Cloudflare during enablement rather than written by hand.

## Applying it

### Dashboard

1. Cloudflare, select `haechi.co.uk`, then **Email** and **Email Routing**.
2. **Enable**. Cloudflare adds the MX records for this zone automatically.
3. Under **Destination addresses**, add `haechi.iss@gmail.com`.
4. Cloudflare sends a verification message to that inbox. Someone must open it
   and confirm. Routing does not work until this is done.
5. Under **Routing rules**, create one custom address per row in the table
   above, each forwarding to the verified destination.

### API

With a token carrying `Zone → Email Routing → Edit`:

```bash
ZONE=ff214640fe230550040b56cd667421a1
ACCOUNT=09087d66cc4fb84c559132f29c11f17b

curl -X POST -H "Authorization: Bearer $CF_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones/$ZONE/email/routing/enable"

curl -X POST -H "Authorization: Bearer $CF_TOKEN" -H 'Content-Type: application/json' \
  --data '{"email":"haechi.iss@gmail.com"}' \
  "https://api.cloudflare.com/client/v4/accounts/$ACCOUNT/email/routing/addresses"

for local in hello admin security license; do
  curl -X POST -H "Authorization: Bearer $CF_TOKEN" -H 'Content-Type: application/json' \
    --data "{\"enabled\":true,\"name\":\"$local\",\"matchers\":[{\"type\":\"literal\",\"field\":\"to\",\"value\":\"$local@haechi.co.uk\"}],\"actions\":[{\"type\":\"forward\",\"value\":[\"haechi.iss@gmail.com\"]}]}" \
    "https://api.cloudflare.com/client/v4/zones/$ZONE/email/routing/rules"
done
```

The destination still requires the human verification step in point 4.

## Interaction with the existing SPF record

The zone currently publishes:

```
haechi.co.uk  TXT  v=spf1 -all
```

That states no host may send as this domain, which is correct while nothing
does. Cloudflare's setup flow may offer to replace it with
`v=spf1 include:_spf.mx.cloudflare.net ~all`.

**Keep `-all` for now.** SPF authorises sending, not receiving, so forwarding
works without the include, and `-all` is the stronger anti-spoofing position.

If DMARC aggregate reports later show forwarding failing SPF, add the include
then. Revisit this the moment outbound sending is configured, because `-all`
will reject your own mail.

## Records that must not be disturbed

| Type | Name           | Value                                                     | Purpose               |
| ---- | -------------- | --------------------------------------------------------- | --------------------- |
| TXT  | `haechi.co.uk` | `v=spf1 -all`                                             | No authorised senders |
| TXT  | `_dmarc`       | `v=DMARC1; p=reject; sp=reject; adkim=s; aspf=s; rua=...` | Reject spoofed mail   |
| TXT  | `*._domainkey` | `v=DKIM1; p=`                                             | All DKIM keys revoked |

Enabling Email Routing adds MX records. It must not remove these three.

## If sending is needed later

Email Routing cannot send. Sending from `@haechi.co.uk` requires a mailbox
provider such as Google Workspace or Microsoft 365, and then:

1. Replace the MX records with the provider's.
2. Change SPF from `-all` to include the provider.
3. Replace the wildcard DKIM revocation with the provider's selector keys.
4. Hold DMARC at `p=reject` only once the provider passes both SPF and DKIM
   alignment, confirmed through aggregate reports.

Doing this in the wrong order sends your first outbound campaign into spam
folders or has it rejected outright.

## Verifying afterwards

```bash
dig +short MX haechi.co.uk                  # Cloudflare route hosts
dig +short TXT haechi.co.uk                 # v=spf1 -all
dig +short TXT _dmarc.haechi.co.uk          # p=reject
```

Then send a live message to each of the four addresses and confirm all arrive
at the destination. Test every address, not just one: rules are per address and
a single typo in a matcher silently drops that address only.
