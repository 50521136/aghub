import { For, Show, createMemo, createSignal, onMount } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { ConfirmDialog } from 'panel/common/ui/ConfirmDialog';
import { Loader } from 'panel/common/ui/Loader';
import { Input } from 'panel/common/controls/Input';
import {
    applyUpdate,
    checkForUpdate,
    getBackup,
    getProxies,
    getUpdateState,
    proxyResultsSorted,
    rollbackUpdate,
    setProxy,
    testProxies,
    updateState,
} from 'panel/stores/update';

import s from './Update.module.pcss';

/** visibleResults is the number of test results shown before the list is
 * expanded. */
const visibleResults = 12;

/** formatDate formats an ISO date string for the current locale. */
const formatDate = (value?: string) => {
    if (!value) {
        return '';
    }

    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return '';
    }

    return date.toLocaleString();
};

/** progressClass returns the width bucket class of the progress bar. */
const progressClass = (value: number) => {
    if (value >= 1) {
        return s.progress100;
    }

    if (value >= 0.75) {
        return s.progress75;
    }

    if (value >= 0.5) {
        return s.progress50;
    }

    if (value >= 0.25) {
        return s.progress25;
    }

    return s.progress0;
};

export const Update = () => {
    const [proxyInput, setProxyInput] = createSignal('');
    const [showAll, setShowAll] = createSignal(false);
    const [rollbackShown, setRollbackShown] = createSignal(false);

    onMount(() => {
        getUpdateState();
        getProxies();
        getBackup();
        // Fill the latest-version card in by itself, so the page does not look
        // broken until someone presses the button.
        checkForUpdate(true);
    });

    // The rollback button names the version it would restore when it is known.
    const rollbackLabel = createMemo(() => {
        const version = updateState.backup?.version;

        return version
            ? intl.getMessage('update_rollback_to', { version })
            : intl.getMessage('update_rollback');
    });

    const rollbackText = createMemo(() => {
        const version = updateState.backup?.version;

        return version
            ? intl.getMessage('update_rollback_confirm', { version })
            : intl.getMessage('update_rollback_confirm_unknown');
    });

    const confirmRollback = () => {
        setRollbackShown(false);
        rollbackUpdate();
    };

    // The input follows the saved value until the user edits it.
    const [edited, setEdited] = createSignal(false);

    const proxyValue = createMemo(() => {
        if (!edited()) {
            return updateState.proxy;
        }

        return proxyInput();
    });

    const results = createMemo(() => proxyResultsSorted(updateState.proxyResults));

    const shownResults = createMemo(() => {
        if (showAll()) {
            return results();
        }

        return results().slice(0, visibleResults);
    });

    const usableCount = createMemo(
        () => results().filter((r) => r.api_ok || r.download_ok).length,
    );

    const latest = createMemo(() => updateState.check?.latest);

    const hasUpdate = createMemo(() => updateState.check?.has_update === true);

    const canUpdate = createMemo(() => updateState.check?.can_update === true);

    const busy = createMemo(() => updateState.checking || updateState.applying);

    const progress = createMemo(() => updateState.status.progress ?? -1);

    return (
        <div class={theme.layout.container}>
            <div class={theme.layout.containerIn}>
                <h1
                    class={cn(theme.layout.title, theme.title.h4, theme.title.h3_tablet)}
                    data-testid="update-title"
                >
                    {intl.getMessage('update_title')}
                </h1>

                <div class={s.body}>
                    <p class={s.desc}>{intl.getMessage('update_desc')}</p>

                    <Show
                        when={updateState.initialized}
                        fallback={<Loader class={s.loader} />}
                    >
                        <div class={s.cards}>
                            <div class={s.card}>
                                <div class={s.cardLabel}>
                                    {intl.getMessage('update_current')}
                                </div>
                                <div class={s.cardValue}>
                                    {updateState.currentVersion || '—'}
                                </div>
                                <Show when={updateState.repository}>
                                    <div class={s.cardHint}>
                                        {intl.getMessage('update_repository')}:{' '}
                                        {updateState.repository}
                                    </div>
                                </Show>
                            </div>

                            <div class={s.card}>
                                <div class={s.cardLabel}>
                                    {intl.getMessage('update_latest_version')}
                                </div>
                                <div class={s.cardValue}>
                                    {latest()?.version ||
                                        intl.getMessage('update_not_checked')}
                                </div>
                                <Show when={latest()?.published_at}>
                                    <div class={s.cardHint}>
                                        {intl.getMessage('update_published')}:{' '}
                                        {formatDate(latest()?.published_at)}
                                    </div>
                                </Show>
                            </div>
                        </div>

                        <Show when={!updateState.enabled}>
                            <div class={s.notice}>
                                {intl.getMessage('update_not_configured')}
                            </div>
                        </Show>

                        <Show when={updateState.check?.error}>
                            <div class={s.error}>{updateState.check?.error}</div>
                        </Show>

                        <Show when={updateState.status.error}>
                            <div class={s.error}>{updateState.status.error}</div>
                        </Show>

                        <div class={s.actions}>
                            <Button
                                size="small"
                                variant="secondary"
                                disabled={busy() || !updateState.enabled}
                                onClick={() => checkForUpdate()}
                                data-testid="update-check"
                            >
                                {intl.getMessage('update_check')}
                            </Button>

                            <Show when={hasUpdate() && canUpdate()}>
                                <Button
                                    size="small"
                                    variant="primary"
                                    disabled={busy()}
                                    onClick={applyUpdate}
                                    data-testid="update-apply"
                                >
                                    {intl.getMessage('update_install')}
                                </Button>
                            </Show>

                            <Show when={updateState.backup?.available}>
                                <Button
                                    size="small"
                                    variant="secondary"
                                    disabled={busy()}
                                    onClick={() => setRollbackShown(true)}
                                    data-testid="update-rollback"
                                >
                                    {rollbackLabel()}
                                </Button>
                            </Show>

                            <Show when={busy()}>
                                <Loader class={s.inlineLoader} />
                            </Show>
                        </div>

                        <Show when={updateState.status.message || updateState.applying}>
                            <div class={s.progressBlock}>
                                <div class={s.progressText}>
                                    {updateState.status.message ||
                                        intl.getMessage('update_working')}
                                </div>
                                <Show when={progress() >= 0}>
                                    <div class={s.progress}>
                                        <div
                                            class={cn(s.progressBar, progressClass(progress()))}
                                        />
                                    </div>
                                </Show>
                                <div class={s.progressHint}>
                                    {intl.getMessage('update_restart_hint')}
                                </div>
                            </div>
                        </Show>

                        <Show when={latest()?.notes}>
                            <div class={s.notesBlock}>
                                <div class={s.notesTitle}>
                                    {intl.getMessage('update_release_notes')}
                                </div>
                                <pre class={s.notes}>{latest()?.notes}</pre>
                            </div>
                        </Show>

                        <div class={s.proxyBlock} data-testid="update-proxy">
                            <div class={s.notesTitle}>
                                {intl.getMessage('update_proxy_title')}
                            </div>
                            <p class={s.proxyDesc}>
                                {intl.getMessage('update_proxy_desc')}
                            </p>

                            <div class={s.proxyRow}>
                                <Input
                                    size="small"
                                    class={s.proxyInput}
                                    value={proxyValue()}
                                    placeholder="https://gh-proxy.com"
                                    onChange={(e) => {
                                        setEdited(true);
                                        setProxyInput(e.currentTarget.value);
                                    }}
                                    data-testid="update-proxy-input"
                                />

                                <Button
                                    size="small"
                                    variant="secondary"
                                    onClick={() => {
                                        setEdited(false);
                                        setProxy('');
                                    }}
                                    data-testid="update-proxy-off"
                                >
                                    {intl.getMessage('update_proxy_direct')}
                                </Button>

                                <Button
                                    size="small"
                                    variant="primary"
                                    disabled={updateState.testingProxies}
                                    onClick={() => {
                                        setShowAll(false);
                                        testProxies();
                                    }}
                                    data-testid="update-proxy-test"
                                >
                                    {intl.getMessage('update_proxy_test')}
                                </Button>

                                <Button
                                    size="small"
                                    variant="secondary"
                                    onClick={() => {
                                        setEdited(false);
                                        setProxy(proxyValue());
                                    }}
                                    data-testid="update-proxy-save"
                                >
                                    {intl.getMessage('update_proxy_save')}
                                </Button>
                            </div>

                            <div class={s.proxyStatus}>
                                {updateState.proxyCurrent
                                    ? intl.getMessage('update_proxy_in_use', {
                                          host: updateState.proxyCurrent,
                                      })
                                    : intl.getMessage('update_proxy_direct_in_use')}
                            </div>

                            <Show when={updateState.testingProxies}>
                                <div class={s.proxyProgress}>
                                    {intl.getMessage('update_proxy_testing', {
                                        done: updateState.proxyTestDone,
                                        total: updateState.proxyTestTotal,
                                    })}
                                </div>
                            </Show>

                            <Show when={!updateState.testingProxies && results().length > 0}>
                                <div class={s.proxySummary}>
                                    {intl.getMessage('update_proxy_summary', {
                                        usable: usableCount(),
                                        total: results().length,
                                    })}
                                </div>
                            </Show>

                            <Show when={shownResults().length > 0}>
                                <div class={s.proxyList}>
                                    <For each={shownResults()}>
                                        {(r) => (
                                            <div
                                                class={cn(s.proxyItem, {
                                                    [s.proxyItemBad]:
                                                        !r.api_ok && !r.download_ok,
                                                })}
                                            >
                                                <span class={s.proxyHost}>{r.host}</span>

                                                <span class={s.proxyLatency}>
                                                    <Show
                                                        when={r.api_ok || r.download_ok}
                                                        fallback="—"
                                                    >
                                                        {r.latency_ms} ms
                                                    </Show>
                                                </span>

                                                <span
                                                    class={cn(s.proxyTag, {
                                                        [s.proxyTagOn]: r.api_ok,
                                                    })}
                                                    title={intl.getMessage(
                                                        'update_proxy_api',
                                                    )}
                                                >
                                                    API
                                                </span>

                                                <span
                                                    class={cn(s.proxyTag, {
                                                        [s.proxyTagOn]: r.download_ok,
                                                    })}
                                                    title={intl.getMessage(
                                                        'update_proxy_download',
                                                    )}
                                                >
                                                    {intl.getMessage('update_proxy_dl')}
                                                </span>

                                                <Button
                                                    size="small"
                                                    variant="secondary"
                                                    class={s.proxyUse}
                                                    onClick={() => {
                                                        setEdited(false);
                                                        setProxy(r.url);
                                                    }}
                                                >
                                                    {intl.getMessage('update_proxy_use')}
                                                </Button>
                                            </div>
                                        )}
                                    </For>
                                </div>

                                <Show when={results().length > visibleResults}>
                                    <Button
                                        size="small"
                                        variant="secondary"
                                        class={s.proxyMore}
                                        onClick={() => setShowAll(!showAll())}
                                    >
                                        {showAll()
                                            ? intl.getMessage('update_proxy_show_less')
                                            : intl.getMessage('update_proxy_show_all', {
                                                  count: results().length,
                                              })}
                                    </Button>
                                </Show>
                            </Show>
                        </div>
                    </Show>
                </div>
            </div>

            <Show when={rollbackShown()}>
                <ConfirmDialog
                    onClose={() => setRollbackShown(false)}
                    onConfirm={confirmRollback}
                    submitDisabled={updateState.rollingBack}
                    buttonText={intl.getMessage('update_rollback')}
                    cancelText={intl.getMessage('cancel')}
                    text={rollbackText()}
                    title={intl.getMessage('update_rollback_confirm_title')}
                    submitTestId="update-rollback-confirm"
                />
            </Show>
        </div>
    );
};
