import { Show, createMemo, onMount } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Loader } from 'panel/common/ui/Loader';
import { applyUpdate, checkForUpdate, getUpdateState, updateState } from 'panel/stores/update';

import s from './Update.module.pcss';

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
    onMount(() => {
        getUpdateState();
    });

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
                                <div class={s.cardValue}>{latest()?.version || '—'}</div>
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
                                onClick={checkForUpdate}
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
                    </Show>
                </div>
            </div>
        </div>
    );
};
