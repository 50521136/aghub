import { For, Show, createSignal, onMount } from 'solid-js';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { ConfirmDialog } from 'panel/common/ui/ConfirmDialog';
import { Dialog } from 'panel/common/ui/Dialog';
import type { BackupInfo } from 'panel/api/model';
import { getBackups, restoreBackup, usersState } from 'panel/stores/users';

import s from './BackupsDialog.module.pcss';

type Props = {
    onClose: () => void;
};

/** formatSize renders a byte count in a compact human-readable form. */
const formatSize = (bytes: number): string => {
    if (bytes < 1024) {
        return `${bytes} B`;
    }

    return `${(bytes / 1024).toFixed(1)} KB`;
};

/** formatTime renders a Unix timestamp in seconds in the local time zone. */
const formatTime = (seconds: number): string => {
    const d = new Date(seconds * 1000);

    const pad = (n: number) => n.toString().padStart(2, '0');

    return (
        `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
        ` ${pad(d.getHours())}:${pad(d.getMinutes())}`
    );
};

export const BackupsDialog = (props: Props) => {
    const [backups, setBackups] = createSignal<BackupInfo[]>([]);
    const [loading, setLoading] = createSignal(true);
    const [pending, setPending] = createSignal<BackupInfo | null>(null);

    const load = async () => {
        setLoading(true);
        setBackups(await getBackups());
        setLoading(false);
    };

    onMount(load);

    const confirmRestore = async () => {
        const target = pending();
        setPending(null);

        if (!target) {
            return;
        }

        if (await restoreBackup(target.name)) {
            await load();
        }
    };

    return (
        <>
            <Dialog
                visible
                onClose={props.onClose}
                title={intl.getMessage('users_backups_title')}
                class={s.dialog}
            >
                <div class={s.body}>
                    <div class={s.hint}>{intl.getMessage('users_backups_hint')}</div>

                    <Show
                        when={backups().length > 0}
                        fallback={
                            <div class={s.empty}>
                                {intl.getMessage(
                                    loading() ? 'users_backups_loading' : 'users_backups_empty',
                                )}
                            </div>
                        }
                    >
                        <ul class={s.list}>
                            <For each={backups()}>
                                {(b) => (
                                    <li class={s.item}>
                                        <div class={s.info}>
                                            <span class={s.time}>{formatTime(b.time)}</span>
                                            <span class={s.meta}>
                                                {b.users >= 0
                                                    ? intl.getMessage('users_backups_users', {
                                                          count: b.users,
                                                      })
                                                    : intl.getMessage('users_backups_unreadable')}
                                                {' · '}
                                                {formatSize(b.size)}
                                            </span>
                                        </div>
                                        <button
                                            type="button"
                                            class={s.restore}
                                            disabled={usersState.processingSave || b.users < 0}
                                            onClick={() => setPending(b)}
                                        >
                                            {intl.getMessage('users_backups_restore')}
                                        </button>
                                    </li>
                                )}
                            </For>
                        </ul>
                    </Show>
                </div>

                <div class={theme.dialog.footer}>
                    <Button
                        variant="secondary"
                        onClick={props.onClose}
                        data-testid="users-backups-close"
                    >
                        {intl.getMessage('close')}
                    </Button>
                </div>
            </Dialog>

            <Show when={pending()}>
                <ConfirmDialog
                    onClose={() => setPending(null)}
                    onConfirm={confirmRestore}
                    submitDisabled={usersState.processingSave}
                    buttonText={intl.getMessage('users_backups_restore')}
                    cancelText={intl.getMessage('cancel')}
                    text={intl.getMessage('users_backups_confirm', {
                        count: pending()?.users ?? 0,
                    })}
                    title={intl.getMessage('users_backups_confirm_title')}
                    submitTestId="users-backups-confirm"
                />
            </Show>
        </>
    );
};
