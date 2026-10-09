# COMPOSITION — platformkit · development

Written by `pkit.Server.Explain` and the server's host claims. Do not edit:
apps/platformkit's TestTheCompositionFileIsCommittedForEachEnvironment refuses a
missing one, and `pkit.Server.Explain` is what writes the text.

App `platformkit` · environment `development` · 16 modules · role `all`.

## Composition

pkit: platformkit in development builds 16 modules.
pkit: a tenant of platformkit begins as coordinator, holding task:read and task:update.
pkit: a tenant of platformkit begins as observer, holding task:read.
pkit: product.Module needs no other module.
pkit: product.Module contributes one tenantcontracts.Hook to tenant.Module.
pkit: user.Module is built after product.Module.
pkit: user.Module needs usercontracts.Administration from product.Module.
pkit: user.Module needs usercontracts.Granting from product.Module.
pkit: user.Module defines the permission user:read, user:manage and user:approve.
pkit: user.Module emits user.user.created, user.user.updated, user.user.deleted, user.invited, user.password_set, user.roles_set, user.deactivated, user.handle_set, user.administration_refused, user.registration_pending, user.registration_approved, user.registration_unverified and user.email_verified.
pkit: site.Module is built after product.Module.
pkit: site.Module uses sitecontracts.WriteGate from product.Module.
pkit: site.Module defines the permission site:manage.
pkit: site.Module emits site.settings_updated.
pkit: tenant.Module is built after product.Module and user.Module.
pkit: tenant.Module needs tenantcontracts.Inviter from user.Module.
pkit: tenant.Module takes every tenantcontracts.Hook from product.Module.
pkit: tenant.Module uses tenantcontracts.Languages from product.Module.
pkit: tenant.Module reads config.NATS.
pkit: tenant.Module reads config.Server.
pkit: tenant.Module defines the permission tenant:manage.
pkit: tenant.Module emits tenant.created, tenant.suspended, tenant.host_added, tenant.locale_set, tenant.renamed, tenant.host_removed, tenant.reactivated, tenant.deleted, tenant.lifecycle_recorded, tenant.oidc_set, tenant.oidc_cleared, tenant.saml_set and tenant.saml_cleared.
pkit: emailregistration.Module is built after user.Module.
pkit: emailregistration.Module needs usercontracts.Service from user.Module.
pkit: notification.Module is built after user.Module and tenant.Module.
pkit: notification.Module needs notificationcontracts.RecipientLookup from user.Module.
pkit: notification.Module needs notificationcontracts.HostLookup from tenant.Module.
pkit: notification.Module reads config.Mail.
pkit: notification.Module reads config.Server.
pkit: notification.Module runs on mailbox, which the deployment picked.
pkit: notification.Module emits notification.created, notification.email_requested and notification.read.
pkit: notification.Module handles notification.email_requested.
pkit: file.Module is built after product.Module and tenant.Module.
pkit: file.Module needs filecontracts.Storage from product.Module.
pkit: file.Module needs jobs.TenantLister from tenant.Module.
pkit: file.Module reads config.Files.
pkit: file.Module defines the permission file:read, file:manage, file:erase and file:retain.
pkit: file.Module emits file.uploaded, file.deleted, file.retained, file.released and file.erased.
pkit: file.Module handles file.deleted.
pkit: task.Module is built after product.Module and tenant.Module.
pkit: task.Module needs jobs.TenantLister from tenant.Module.
pkit: task.Module uses tenancy.Policy from product.Module.
pkit: task.Module defines the permission task:read and task:update.
pkit: task.Module emits task.task.created, task.task.updated, task.task.deleted, task.assigned, task.resolved and task.sla_breached.
pkit: billing.Module is built after tenant.Module.
pkit: billing.Module needs jobs.TenantLister from tenant.Module.
pkit: billing.Module runs on manual, which the deployment picked.
pkit: billing.Module defines the permission billing:read, billing:manage and billing:catalog.
pkit: billing.Module emits billing.plan.created, billing.plan.updated, billing.plan.deleted, billing.subscribed, billing.cancelled, billing.renewed and billing.past_due.
pkit: audit.Module is built after product.Module and tenant.Module.
pkit: audit.Module needs jobs.TenantLister from tenant.Module.
pkit: audit.Module uses auditcontracts.Plan from product.Module.
pkit: audit.Module reads config.Audit.
pkit: audit.Module reads config.Database.
pkit: audit.Module defines the permission audit:read.
pkit: audit.Module handles every event this application emits.
pkit: auth.Module is built after product.Module, user.Module, tenant.Module, notification.Module and emailregistration.Module.
pkit: auth.Module needs authcontracts.Users from user.Module.
pkit: auth.Module needs authcontracts.Provisioner from user.Module.
pkit: auth.Module needs authcontracts.Notifier from notification.Module.
pkit: auth.Module needs notificationcontracts.Mailer from notification.Module.
pkit: auth.Module needs notificationcontracts.HostLookup from tenant.Module.
pkit: auth.Module needs authcontracts.OIDCProviders from tenant.Module.
pkit: auth.Module needs jobs.TenantLister from tenant.Module.
pkit: auth.Module needs usercontracts.Granting from product.Module.
pkit: auth.Module uses authcontracts.SAMLProviders from tenant.Module.
pkit: auth.Module uses authcontracts.RegistrationMode from emailregistration.Module.
pkit: auth.Module reads config.Auth.
pkit: auth.Module reads config.Server.
pkit: auth.Module defines the permission role:manage.
pkit: auth.Module emits auth.logged_in, auth.logged_out, auth.login_failed, auth.reset_requested, auth.password_reset, auth.role_set, auth.session_revoked, auth.factor_enrolled, auth.factor_withdrawn, auth.recovery_codes_issued, auth.recovery_code_used, auth.api_token_issued, auth.api_token_revoked, auth.registration_requested, auth.verification_requested and auth.administration_refused.
pkit: auth.Module handles user.invited, auth.reset_requested, user.registration_unverified and auth.verification_requested.
pkit: access.Module is built after product.Module, user.Module, notification.Module and site.Module.
pkit: access.Module needs usercontracts.Service from user.Module.
pkit: access.Module needs notificationcontracts.Service from notification.Module.
pkit: access.Module needs sitecontracts.Service from site.Module.
pkit: access.Module needs usercontracts.Granting from product.Module.
pkit: access.Module contributes one changecontracts.SubjectBinding to change.Module.
pkit: content.Module is built after file.Module.
pkit: content.Module needs richtext.Files from file.Module.
pkit: content.Module needs rest.FileUses from file.Module.
pkit: content.Module defines the permission content:read and content:manage.
pkit: content.Module emits content.content.created, content.content.updated, content.content.deleted, content.published, content.unpublished and content.archived.
pkit: change.Module is built after access.Module.
pkit: change.Module takes every changecontracts.SubjectBinding from access.Module.
pkit: change.Module defines the permission change:read, change:propose and change:decide.
pkit: change.Module emits change.proposal_proposed, change.proposal_reviewed, change.proposal_applied and change.proposal_withdrawn.
pkit: web.Module is built after product.Module, file.Module, content.Module and site.Module.
pkit: web.Module needs sitecontracts.Service from site.Module.
pkit: web.Module needs contentcontracts.Service from content.Module.
pkit: web.Module needs richtext.Files from file.Module.
pkit: web.Module uses webcontracts.Links from product.Module.
pkit: admin.Module needs authcontracts.Auth from auth.Module.
pkit: admin.Module needs tenantcontracts.Service from tenant.Module.
pkit: admin.Module uses admincontracts.Signin from product.Module.
pkit: admin.Module uses admincontracts.Locale from product.Module.
pkit: admin.Module could use admincontracts.Storybook; platformkit composes no provider, so it will not.
pkit: admin.Module runs after everything and saw every other module's manifest.
pkit: admin.Module defines the permission gallery:read.
pkit: tenant.Module provides httpx.TenantLoader; no module in platformkit needs it.
pkit: file.Module provides filecontracts.Service; no module in platformkit needs it.
pkit: task.Module provides taskcontracts.Service; no module in platformkit needs it.
pkit: billing.Module provides httpx.Entitler; no module in platformkit needs it.
pkit: auth.Module provides httpx.Authorizer; no module in platformkit needs it.
pkit: auth.Module provides pkit.Authenticator; no module in platformkit needs it.
pkit: change.Module provides changecontracts.Service; no module in platformkit needs it.

## What the application answers the kernel

- which host is which tenant: tenant.Module
- what they may do: auth.Module
- who is calling: auth.Module
- what their plan includes: billing.Module
- how a refusal looks: the application's own ErrorPage
- how a person asks for what was refused: the ask door and its page
- the installation itself, and the control plane: installation.platformkit.localhost (from `server.installation_host`)

## Tenant hosts

- platformkit — platformkit.localhost (claimed; the tenant rows decide who is served)
