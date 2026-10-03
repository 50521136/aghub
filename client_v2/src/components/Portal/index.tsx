import { createSignal, onMount } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Input } from 'panel/common/controls/Input';
import { Switch } from 'panel/common/controls/Switch';
import { Textarea } from 'panel/common/controls/Textarea';
import { portalMailTest } from 'panel/api/generated';
import { addErrorToast, addSuccessToast } from 'panel/stores/toasts';
import { downloadPortalPackage, getUsers, saveSettings, usersState } from 'panel/stores/users';

import s from './Portal.module.pcss';

/** toLines splits a textarea value into trimmed non-empty lines. */
const toLines = (value: string): string[] =>
    value
        .split('\n')
        .map((line) => line.trim())
        .filter(Boolean);

/** toCount parses a number field, treating anything unusable as zero. */
const toCount = (value: string): number => {
    const n = Number.parseInt(value, 10);

    return Number.isFinite(n) && n > 0 ? n : 0;
};

export const Portal = () => {
    const [open, setOpen] = createSignal(false);
    const [emailVerify, setEmailVerify] = createSignal(false);
    const [quota, setQuota] = createSignal('0');
    const [days, setDays] = createSignal('0');
    const [announcement, setAnnouncement] = createSignal('');

    const [apiBase, setApiBase] = createSignal('');
    const [origins, setOrigins] = createSignal('');

    const [smtpHost, setSmtpHost] = createSignal('');
    const [smtpPort, setSmtpPort] = createSignal('');
    const [smtpUser, setSmtpUser] = createSignal('');
    const [smtpPassword, setSmtpPassword] = createSignal('');
    const [smtpFrom, setSmtpFrom] = createSignal('');
    const [smtpPlain, setSmtpPlain] = createSignal(false);
    const [testTo, setTestTo] = createSignal('');

    const [saving, setSaving] = createSignal(false);
    const [testing, setTesting] = createSignal(false);

    /** fill copies the stored settings into the form. */
    const fill = () => {
        const c = usersState.settings;

        setOpen(!!c.portal_open);
        setEmailVerify(!!c.portal_email_verify);
        setQuota(String(c.portal_default_quota ?? 0));
        setDays(String(c.portal_default_days ?? 0));
        setAnnouncement(c.portal_announcement ?? '');

        setApiBase(c.portal_api_base ?? '');
        setOrigins((c.portal_origins ?? []).join('\n'));

        setSmtpHost(c.smtp_host ?? '');
        setSmtpPort(c.smtp_port ? String(c.smtp_port) : '');
        setSmtpUser(c.smtp_user ?? '');
        setSmtpPassword('');
        setSmtpFrom(c.smtp_from ?? '');
        setSmtpPlain(!!c.smtp_plain);
    };

    onMount(async () => {
        await getUsers();
        fill();
    });

    const save = async (): Promise<boolean> => {
        setSaving(true);

        const ok = await saveSettings({
            portal_open: open(),
            portal_email_verify: emailVerify(),
            portal_default_quota: toCount(quota()),
            portal_default_days: toCount(days()),
            portal_announcement: announcement(),
            portal_api_base: apiBase().trim(),
            portal_origins: toLines(origins()),
            smtp_host: smtpHost().trim(),
            smtp_port: toCount(smtpPort()),
            smtp_user: smtpUser().trim(),
            // An empty password means "keep the stored one", so that the host
            // can be changed without retyping the secret.
            smtp_password: smtpPassword(),
            smtp_from: smtpFrom().trim(),
            smtp_plain: smtpPlain(),
        });

        setSaving(false);

        if (ok) {
            setSmtpPassword('');
            fill();
        }

        return ok;
    };

    const test = async () => {
        const to = testTo().trim();
        if (!to) {
            addErrorToast(intl.getMessage('portal_mail_test_need_address'));

            return;
        }

        setTesting(true);

        try {
            await portalMailTest({ to });
            addSuccessToast(intl.getMessage('portal_mail_test_sent'));
        } catch (error) {
            addErrorToast({ error });
        } finally {
            setTesting(false);
        }
    };

    return (
        <div class={s.page}>
            <h1 class={cnTitle()}>{intl.getMessage('portal_title')}</h1>
            <p class={cnHint()}>{intl.getMessage('portal_description')}</p>

            <section class={s.card}>
                <h2 class={cnCardTitle()}>{intl.getMessage('portal_signup_title')}</h2>

                <Switch
                    id="portal-open"
                    checked={open()}
                    onChange={() => setOpen((prev) => !prev)}
                >
                    {intl.getMessage('portal_open')}
                </Switch>
                <p class={cnHint()}>{intl.getMessage('portal_open_hint')}</p>

                <Switch
                    id="portal-email-verify"
                    checked={emailVerify()}
                    onChange={() => setEmailVerify((prev) => !prev)}
                >
                    {intl.getMessage('portal_email_verify')}
                </Switch>
                <p class={cnHint()}>{intl.getMessage('portal_email_verify_hint')}</p>

                <div class={s.row}>
                    <Input
                        label={intl.getMessage('portal_default_quota')}
                        value={quota()}
                        onInput={(e) => setQuota(e.currentTarget.value)}
                    />
                    <Input
                        label={intl.getMessage('portal_default_days')}
                        value={days()}
                        onInput={(e) => setDays(e.currentTarget.value)}
                    />
                </div>
                <p class={cnHint()}>{intl.getMessage('portal_defaults_hint')}</p>
            </section>

            <section class={s.card}>
                <h2 class={cnCardTitle()}>{intl.getMessage('portal_announcement')}</h2>

                <Textarea
                    value={announcement()}
                    onInput={(e) => setAnnouncement(e.currentTarget.value)}
                    data-testid="portal-announcement"
                />
                <p class={cnHint()}>{intl.getMessage('portal_announcement_hint')}</p>
            </section>

            <section class={s.card}>
                <h2 class={cnCardTitle()}>{intl.getMessage('portal_mail_title')}</h2>
                <p class={cnHint()}>{intl.getMessage('portal_mail_hint')}</p>

                <div class={s.row}>
                    <Input
                        label={intl.getMessage('portal_smtp_host')}
                        value={smtpHost()}
                        onInput={(e) => setSmtpHost(e.currentTarget.value)}
                    />
                    <Input
                        label={intl.getMessage('portal_smtp_port')}
                        value={smtpPort()}
                        onInput={(e) => setSmtpPort(e.currentTarget.value)}
                    />
                </div>

                <div class={s.row}>
                    <Input
                        label={intl.getMessage('portal_smtp_user')}
                        value={smtpUser()}
                        onInput={(e) => setSmtpUser(e.currentTarget.value)}
                    />
                    <Input
                        label={intl.getMessage('portal_smtp_password')}
                        type="password"
                        placeholder={
                            usersState.settings.smtp_password_set
                                ? intl.getMessage('portal_smtp_password_kept')
                                : ''
                        }
                        value={smtpPassword()}
                        onInput={(e) => setSmtpPassword(e.currentTarget.value)}
                    />
                </div>

                <Input
                    label={intl.getMessage('portal_smtp_from')}
                    value={smtpFrom()}
                    onInput={(e) => setSmtpFrom(e.currentTarget.value)}
                />

                <Switch
                    id="portal-smtp-plain"
                    checked={smtpPlain()}
                    onChange={() => setSmtpPlain((prev) => !prev)}
                >
                    {intl.getMessage('portal_smtp_plain')}
                </Switch>

                <div class={s.row}>
                    <Input
                        label={intl.getMessage('portal_mail_test_to')}
                        value={testTo()}
                        onInput={(e) => setTestTo(e.currentTarget.value)}
                    />
                    <Button
                        variant="secondary"
                        disabled={testing()}
                        onClick={test}
                        data-testid="portal-mail-test"
                    >
                        {intl.getMessage('portal_mail_test')}
                    </Button>
                </div>
            </section>

            <section class={s.card}>
                <h2 class={cnCardTitle()}>{intl.getMessage('portal_deploy_title')}</h2>
                <p class={cnHint()}>{intl.getMessage('portal_deploy_hint')}</p>

                <Input
                    label={intl.getMessage('portal_api_base')}
                    value={apiBase()}
                    onInput={(e) => setApiBase(e.currentTarget.value)}
                    data-testid="portal-api-base"
                />

                <Textarea
                    label={intl.getMessage('portal_origins')}
                    value={origins()}
                    onInput={(e) => setOrigins(e.currentTarget.value)}
                    data-testid="portal-origins"
                />
                <p class={cnHint()}>{intl.getMessage('portal_origins_hint')}</p>

                <Button
                    variant="secondary"
                    onClick={() => {
                        // The address is baked into the package, so it has to be
                        // saved before the archive is built.
                        void save().then((ok) => {
                            if (ok) {
                                void downloadPortalPackage();
                            }
                        });
                    }}
                    data-testid="portal-download"
                >
                    {intl.getMessage('portal_download')}
                </Button>
            </section>

            <div class={s.footer}>
                <Button
                    variant="primary"
                    disabled={saving()}
                    onClick={() => void save()}
                    data-testid="portal-save"
                >
                    {intl.getMessage('save')}
                </Button>
            </div>
        </div>
    );
};

/** cnTitle is the page title class list. */
const cnTitle = () => cn(theme.title.h4, s.title);

/** cnCardTitle is the card heading class list. */
const cnCardTitle = () => cn(theme.title.h6, s.cardTitle);

/** cnHint is the hint paragraph class list. */
const cnHint = () => cn(theme.text.t3, s.hint);
