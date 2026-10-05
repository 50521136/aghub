import { createSignal, For, onMount, Show } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Input } from 'panel/common/controls/Input';
import { Switch } from 'panel/common/controls/Switch';
import { Textarea } from 'panel/common/controls/Textarea';
import {
    portalFeedbackDelete,
    portalFeedbackList,
    portalFeedbackRead,
    portalMailTest,
} from 'panel/api/generated';
import type { PortalFeedback } from 'panel/api/generated';
import { addErrorToast, addSuccessToast } from 'panel/stores/toasts';
import { downloadPortalPackage, getUsers, rotatePortalToken, saveSettings, usersState } from 'panel/stores/users';

import s from './Portal.module.pcss';

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
    const [token, setToken] = createSignal('');

    const [smtpHost, setSmtpHost] = createSignal('');
    const [smtpPort, setSmtpPort] = createSignal('');
    const [smtpUser, setSmtpUser] = createSignal('');
    const [smtpPassword, setSmtpPassword] = createSignal('');
    const [smtpFrom, setSmtpFrom] = createSignal('');
    const [smtpPlain, setSmtpPlain] = createSignal(false);
    const [testTo, setTestTo] = createSignal('');

    const [saving, setSaving] = createSignal(false);
    const [testing, setTesting] = createSignal(false);

    const [feedback, setFeedback] = createSignal<PortalFeedback[]>([]);
    const [unread, setUnread] = createSignal(0);
    const [fbLoading, setFbLoading] = createSignal(false);

    /**
     * loadFeedback pulls the messages users left in the portal.  They are not
     * part of the public API, so this is the only place they can be read.
     */
    const loadFeedback = async () => {
        setFbLoading(true);

        try {
            const r = await portalFeedbackList();

            setFeedback(r.items ?? []);
            setUnread(r.unread ?? 0);
        } catch (e: unknown) {
            addErrorToast(intl.getMessage('portal_feedback_load_failed'));
            console.error(e);
        } finally {
            setFbLoading(false);
        }
    };

    /** markRead clears the unread badge without hiding the messages. */
    const markRead = async () => {
        try {
            await portalFeedbackRead();
            setUnread(0);
            setFeedback(feedback().map((f) => ({ ...f, read: true })));
        } catch (e: unknown) {
            addErrorToast(intl.getMessage('portal_feedback_load_failed'));
            console.error(e);
        }
    };

    const removeFeedback = async (id: string) => {
        try {
            await portalFeedbackDelete({ id });
            setFeedback(feedback().filter((f) => f.id !== id));
        } catch (e: unknown) {
            addErrorToast(intl.getMessage('portal_feedback_load_failed'));
            console.error(e);
        }
    };

    /** fill copies the stored settings into the form. */
    const fill = () => {
        const c = usersState.settings;

        setOpen(!!c.portal_open);
        setEmailVerify(!!c.portal_email_verify);
        setQuota(String(c.portal_default_quota ?? 0));
        setDays(String(c.portal_default_days ?? 0));
        setAnnouncement(c.portal_announcement ?? '');

        setApiBase(c.portal_api_base ?? '');
        setToken(c.portal_token ?? '');

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
        await loadFeedback();
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
            // 对接令牌由 AGHub 生成，不由表单提交：前端包里的 token 必须和
            // 服务端一致，能改的话一次误操作就让所有已部署的前端失效。

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
                <p class={cnHint()}>{intl.getMessage('portal_api_base_hint')}</p>

                <Input
                    label={intl.getMessage('portal_token')}
                    value={token()}
                    readOnly
                    data-testid="portal-token"
                />
                <p class={cnHint()}>{intl.getMessage('portal_token_hint')}</p>

                <Button
                    variant="secondary"
                    disabled={usersState.processingSave}
                    onClick={() => void rotatePortalToken().then((t) => setToken(t))}
                    data-testid="portal-token-rotate"
                >
                    {intl.getMessage('portal_token_rotate')}
                </Button>

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

            <section class={s.card}>
                <h2 class={cnCardTitle()}>
                    {intl.getMessage('portal_feedback_title')}
                    <Show when={unread() > 0}>
                        {' '}
                        <span class={cn(theme.text.t4, s.badge)} data-testid="portal-feedback-unread">
                            {unread()}
                        </span>
                    </Show>
                </h2>
                <p class={cnHint()}>{intl.getMessage('portal_feedback_hint')}</p>

                <Show when={feedback().length > 0} fallback={<p class={cnHint()}>{intl.getMessage('portal_feedback_empty')}</p>}>
                    <ul class={s.feedbackList} data-testid="portal-feedback-list">
                        <For each={feedback()}>
                            {(f) => (
                                <li class={cn(s.feedbackItem, { [s.feedbackNew]: !f.read })}>
                                    <div class={s.feedbackHead}>
                                        <span class={cn(theme.text.t3, theme.text.medium, s.feedbackWho)}>
                                            {f.name || f.uid}
                                            <span class={cn(theme.text.t4, s.feedbackId)}>{f.uid}</span>
                                        </span>
                                        <span class={cn(theme.text.t4, s.feedbackTime)}>{formatTime(f.created_at)}</span>
                                    </div>
                                    <Show when={f.contact}>
                                        <span class={cn(theme.text.t4, s.feedbackMail)}>{f.contact}</span>
                                    </Show>
                                    <p class={cn(theme.text.t3, s.feedbackText)}>{f.content}</p>
                                    <Button
                                        variant="secondary"
                                        onClick={() => void removeFeedback(f.id)}
                                    >
                                        {intl.getMessage('portal_feedback_delete')}
                                    </Button>
                                </li>
                            )}
                        </For>
                    </ul>

                    <Button
                        variant="secondary"
                        disabled={unread() === 0}
                        onClick={() => void markRead()}
                        data-testid="portal-feedback-read"
                    >
                        {intl.getMessage('portal_feedback_mark_read')}
                    </Button>
                </Show>

                <Button
                    variant="secondary"
                    disabled={fbLoading()}
                    onClick={() => void loadFeedback()}
                    data-testid="portal-feedback-reload"
                >
                    {intl.getMessage('portal_feedback_reload')}
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

/** formatTime renders a Unix timestamp in seconds as a local date and time. */
const formatTime = (seconds: number): string => {
    if (!seconds) {
        return '';
    }

    return new Date(seconds * 1000).toLocaleString();
};

/** cnTitle is the page title class list. */
const cnTitle = () => cn(theme.title.h4, s.title);

/** cnCardTitle is the card heading class list. */
const cnCardTitle = () => cn(theme.title.h6, s.cardTitle);

/** cnHint is the hint paragraph class list. */
const cnHint = () => cn(theme.text.t3, s.hint);
