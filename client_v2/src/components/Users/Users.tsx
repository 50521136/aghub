import { For, Show, createMemo, createSignal, onMount } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Checkbox } from 'panel/common/controls/Checkbox';
import { Input } from 'panel/common/controls/Input';
import { ConfirmDialog } from 'panel/common/ui/ConfirmDialog';
import { Icon } from 'panel/common/ui/Icon';
import { PlusButton } from 'panel/common/ui/PlusButton';
import { Table, type TableColumn } from 'panel/common/ui/Table';
import { Tooltip } from 'panel/common/ui/Tooltip';
import type { User } from 'panel/api/model';
import { deleteUsers, getUsers, resetUsers, toggleUsers, usersState } from 'panel/stores/users';
import { addErrorToast, addSuccessToast } from 'panel/stores/toasts';

import { BulkAddDialog } from './blocks/BulkAddDialog';
import { BackupsDialog } from './blocks/BackupsDialog';
import { PortalPasswordDialog } from './blocks/PortalPasswordDialog';
import { UserDialog } from './blocks/UserDialog';
import s from './Users.module.pcss';

/** Statuses that a user can have, in the order of the filter. */
const STATUSES = ['active', 'expiring_soon', 'over_quota', 'expired', 'disabled'] as const;

/** statusText returns the localized label of a status. */
const statusText = (status?: string) => {
    switch (status) {
        case 'active':
            return intl.getMessage('users_status_active');
        case 'expiring_soon':
            return intl.getMessage('users_status_expiring_soon');
        case 'over_quota':
            return intl.getMessage('users_status_over_quota');
        case 'expired':
            return intl.getMessage('users_status_expired');
        case 'disabled':
            return intl.getMessage('users_status_disabled');
        default:
            return status || '';
    }
};

/** statusClass returns the badge modifier class of a status. */
const statusClass = (status?: string) => {
    switch (status) {
        case 'active':
            return s.badgeActive;
        case 'expiring_soon':
            return s.badgeWarn;
        case 'over_quota':
        case 'expired':
            return s.badgeErr;
        default:
            return s.badgeMuted;
    }
};

/** formatDate formats a Unix timestamp in seconds as a local date. */
const formatDate = (seconds?: number) => {
    if (!seconds || seconds < 0) {
        return intl.getMessage('users_never');
    }

    const d = new Date(seconds * 1000);

    return d.toLocaleDateString(undefined, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
    });
};

/** formatDays formats a number of seconds as a human-readable duration. */
const formatDays = (seconds?: number) => {
    if (seconds === undefined || seconds < 0) {
        return intl.getMessage('users_unlimited');
    }

    // A zero remaining time means that the user has just expired, so showing
    // "1 min" would be misleading.
    if (seconds === 0) {
        return intl.getMessage('users_status_expired');
    }

    const days = Math.floor(seconds / 86400);
    if (days > 0) {
        return intl.getMessage('users_days', { count: days });
    }

    const hours = Math.floor(seconds / 3600);
    if (hours > 0) {
        return intl.getMessage('users_hours', { count: hours });
    }

    return intl.getMessage('users_minutes', { count: Math.max(1, Math.floor(seconds / 60)) });
};

/** formatNumber formats a number with the locale separators. */
const formatNumber = (value?: number) => (value ?? 0).toLocaleString();

/**
 * hostName joins an identifier to the DoT/DoH domain, which is the host name a
 * client has to put in its SNI or DoH URL.  The identifier alone is only half
 * of it.
 */
const hostName = (id: string) => {
    const domain = usersState.domain;

    return domain ? `${id}.${domain}` : id;
};

/** idsText returns the label of the identifier button of a user. */
const idsText = (u: User) => {
    const ids = u.ids || [];
    if (ids.length === 0) {
        return '';
    }

    if (ids.length === 1) {
        return ids[0];
    }

    return intl.getMessage('users_ids_count', { count: ids.length });
};

/**
 * copyText writes text to the clipboard.  The asynchronous clipboard API is
 * only available in a secure context, and this panel is commonly reached over
 * plain HTTP, so a hidden text area and the legacy command are the fallback.
 */
const copyText = async (text: string): Promise<boolean> => {
    if (window.isSecureContext && navigator.clipboard) {
        try {
            await navigator.clipboard.writeText(text);

            return true;
        } catch {
            // Fall through to the legacy path.
        }
    }

    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.top = '-1000px';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();

    let ok = false;
    try {
        ok = document.execCommand('copy');
    } catch {
        ok = false;
    }

    document.body.removeChild(area);

    return ok;
};

/** copyIDs copies the full host names of a user to the clipboard. */
const copyIDs = async (u: User) => {
    const text = (u.ids || []).map(hostName).join('\n');
    const ok = await copyText(text);

    if (ok) {
        addSuccessToast(intl.getMessage('users_copied', { host: hostName((u.ids || [])[0] || '') }));
    } else {
        addErrorToast({ error: new Error(intl.getMessage('users_copy_failed')) });
    }
};

/**
 * resetText returns the moment the request counter of a user goes back to zero.
 * The boundaries are midnight-aligned and do not depend on when the user was
 * created, so a daily quota always resets at the next midnight.  An empty
 * string means that the quota is counted over the whole lifetime.
 */
const resetText = (u: User) => {
    const next = u.next_reset ?? 0;
    if (next <= 0) {
        return '';
    }

    const date = new Date(next * 1000);
    const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });

    if (u.period === 'month') {
        return intl.getMessage('users_reset_at_date', {
            date: date.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit' }),
            time,
        });
    }

    return intl.getMessage('users_reset_at', { time });
};

/**
 * progressClass returns the width bucket class of the usage progress bar.  The
 * width is applied through a class rather than an inline style so that all the
 * styling stays in the CSS module.
 */
const progressClass = (percent: number): string => {
    if (percent >= 100) {
        return s.progress100;
    }

    if (percent >= 75) {
        return s.progress75;
    }

    if (percent >= 50) {
        return s.progress50;
    }

    if (percent >= 25) {
        return s.progress25;
    }

    return s.progress0;
};

export const Users = () => {
    const [search, setSearch] = createSignal('');
    const [statusFilter, setStatusFilter] = createSignal<string>('all');
    const [selected, setSelected] = createSignal<string[]>([]);
    const [edited, setEdited] = createSignal<User | null | undefined>(undefined);
    const [confirmKind, setConfirmKind] = createSignal<'delete' | 'reset' | ''>('');
    const [bulkAddShown, setBulkAddShown] = createSignal(false);
    const [backupsShown, setBackupsShown] = createSignal(false);
    const [portalUser, setPortalUser] = createSignal<User | null>(null);

    onMount(() => {
        getUsers();
    });

    const filtered = createMemo(() => {
        const query = search().trim().toLowerCase();
        const status = statusFilter();

        return (usersState.users || []).filter((u) => {
            if (status !== 'all' && u.status !== status) {
                return false;
            }

            if (!query) {
                return true;
            }

            const haystack = [u.name, u.remark, u.uid, ...(u.ids || [])]
                .filter(Boolean)
                .join(' ')
                .toLowerCase();

            return haystack.includes(query);
        });
    });

    const allSelected = createMemo(
        () => filtered().length > 0 && selected().length === filtered().length,
    );

    const toggleSelect = (uid: string) => {
        setSelected((prev) =>
            prev.includes(uid) ? prev.filter((id) => id !== uid) : [...prev, uid],
        );
    };

    const toggleSelectAll = () => {
        if (allSelected()) {
            setSelected([]);

            return;
        }

        setSelected(filtered().map((u) => u.uid || '').filter(Boolean));
    };

    const handleConfirm = async () => {
        const uids = selected();
        const kind = confirmKind();

        setConfirmKind('');
        setSelected([]);

        if (kind === 'delete') {
            await deleteUsers(uids);
        } else if (kind === 'reset') {
            await resetUsers(uids);
        }
    };

    /** quotaPercent returns the usage percentage, or -1 for unlimited users. */
    const quotaPercent = (u: User) => {
        const limit = u.request_limit ?? -1;
        if (limit < 0) {
            return -1;
        }

        if (limit === 0) {
            return 100;
        }

        return Math.min(100, Math.round(((u.requests ?? 0) / limit) * 100));
    };

    /** limitText returns the localized quota of a user. */
    const limitText = (u: User) => {
        const limit = u.request_limit ?? -1;

        return limit < 0 ? intl.getMessage('users_unlimited') : formatNumber(limit);
    };

    /** statusCount returns the number of users in a given status. */
    const statusCount = (key: string) => {
        const summary = usersState.summary as unknown as Record<string, number>;

        return summary[key] ?? 0;
    };

    const columns = createMemo(
        (): TableColumn<User>[] => [
            {
                key: 'select',
                width: 36,
                sortable: false,
                class: s.selectCellOuter,
                header: {
                    text: '',
                    render: () => (
                        <Checkbox
                            checked={allSelected()}
                            onChange={toggleSelectAll}
                            data-testid="users-select-all"
                        />
                    ),
                },
                render: (_value, u) => (
                    <div class={s.selectCell}>
                        <Checkbox
                            checked={selected().includes(u.uid || '')}
                            onChange={() => toggleSelect(u.uid || '')}
                        />
                    </div>
                ),
            },
            {
                // The identifiers belong to the same client as the name, so
                // they share this column: a card then needs one line for the
                // pair instead of two.
                key: 'name',
                accessor: 'name',
                minWidth: 220,
                header: { text: intl.getMessage('users_col_name') },
                render: (_value, u) => (
                    <div class={theme.table.cell}>
                        <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                            {intl.getMessage('users_col_name')}
                        </div>
                        <div class={theme.table.cellValue}>
                            <div class={s.nameBox}>
                                <span class={s.nameLine}>
                                    <span class={s.nameText}>{u.name}</span>
                                    <Show when={u.remark}>
                                        <span class={s.remarkText} title={u.remark}>
                                            {u.remark}
                                        </span>
                                    </Show>
                                </span>
                                <Tooltip
                                    class={s.idsWrap}
                                    overlayClass={s.idsTooltipOverlay}
                                    content={
                                        <div class={s.idsTooltip}>
                                            <div class={s.idsTooltipHint}>
                                                {intl.getMessage('users_copy_hint')}
                                            </div>
                                            <For each={u.ids || []}>
                                                {(id) => <div>{hostName(id)}</div>}
                                            </For>
                                        </div>
                                    }
                                >
                                    <button
                                        type="button"
                                        class={s.idsButton}
                                        onClick={() => copyIDs(u)}
                                    >
                                        {idsText(u)}
                                    </button>
                                </Tooltip>
                            </div>
                        </div>
                    </div>
                ),
            },
            {
                key: 'usage',
                accessor: (u) => u.requests ?? 0,
                minWidth: 110,
                maxWidth: 180,
                header: { text: intl.getMessage('users_col_usage') },
                render: (_value, u) => {
                    const percent = quotaPercent(u);
                    const reset = resetText(u);

                    return (
                        <div class={theme.table.cell}>
                            <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                                {intl.getMessage('users_col_usage')}
                            </div>
                            <div class={theme.table.cellValue}>
                                <div class={s.usageBox}>
                                    <div class={s.usageLine}>
                                        <span class={s.usageText}>
                                            {formatNumber(u.requests)} / {limitText(u)}
                                        </span>
                                        <Show when={percent >= 0}>
                                            <span class={s.progress}>
                                                <span
                                                    class={cn(
                                                        s.progressBar,
                                                        progressClass(percent),
                                                    )}
                                                />
                                            </span>
                                        </Show>
                                    </div>
                                    {/* The counter is cleared at a fixed midnight
                                        boundary, so it is worth showing when. */}
                                    <Show when={reset}>
                                        <span class={s.resetText}>{reset}</span>
                                    </Show>
                                </div>
                            </div>
                        </div>
                    );
                },
            },
            {
                // The remaining time and the status describe the same thing —
                // whether the user can still query — so they share a column and
                // a card needs one line for the pair.
                key: 'remaining',
                accessor: (u) => u.remaining_seconds ?? -1,
                minWidth: 150,
                header: { text: intl.getMessage('users_col_remaining') },
                render: (_value, u) => (
                    <div class={theme.table.cell}>
                        {/* The card layout has no column header, so the label
                            carries the exact expiration date there. */}
                        <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                            {intl.getMessage('users_col_expires')} {formatDate(u.expires_at)}
                        </div>
                        <div class={theme.table.cellValue}>
                            <div class={s.remainingBox}>
                                <Tooltip
                                    content={`${intl.getMessage('users_col_expires')}: ${formatDate(
                                        u.expires_at,
                                    )}`}
                                >
                                    <div
                                        class={cn(theme.table.cellValueText, {
                                            [s.textWarn]: u.status === 'expiring_soon',
                                            [s.textErr]: u.status === 'expired',
                                        })}
                                    >
                                        {formatDays(u.remaining_seconds)}
                                    </div>
                                </Tooltip>
                                <span class={cn(s.badge, statusClass(u.status))}>
                                    {statusText(u.status)}
                                </span>
                            </div>
                        </div>
                    </div>
                ),
            },
            {
                key: 'actions',
                width: 120,
                sortable: false,
                class: s.actionsCell,
                header: {
                    text: intl.getMessage('users_col_actions'),
                    // The action buttons are right-aligned in their cell, so
                    // the header label has to be right-aligned as well.
                    render: () => (
                        <span class={s.actionsHeader}>
                            {intl.getMessage('users_col_actions')}
                        </span>
                    ),
                },
                render: (_value, u) => (
                    <div class={s.actions}>
                        <Tooltip content={intl.getMessage('users_edit')}>
                            <button
                                type="button"
                                class={theme.table.action}
                                onClick={() => setEdited(u)}
                            >
                                <Icon icon="edit" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('users_edit')}
                                </span>
                            </button>
                        </Tooltip>

                        <Tooltip
                            content={
                                u.status === 'disabled'
                                    ? intl.getMessage('users_enable')
                                    : intl.getMessage('users_disable')
                            }
                        >
                            <button
                                type="button"
                                class={theme.table.action}
                                disabled={usersState.processingSave}
                                onClick={() =>
                                    toggleUsers([u.uid || ''], u.status === 'disabled')
                                }
                            >
                                <Icon icon={u.status === 'disabled' ? 'check' : 'lock'} />
                                <span class={theme.table.actionLabel}>
                                    {u.status === 'disabled'
                                        ? intl.getMessage('users_enable')
                                        : intl.getMessage('users_disable')}
                                </span>
                            </button>
                        </Tooltip>

                        <Tooltip content={intl.getMessage('users_reset')}>
                            <button
                                type="button"
                                class={theme.table.action}
                                disabled={usersState.processingSave}
                                onClick={() => {
                                    setSelected([u.uid || '']);
                                    setConfirmKind('reset');
                                }}
                            >
                                <Icon icon="refresh" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('users_reset')}
                                </span>
                            </button>
                        </Tooltip>

                        <Tooltip content={intl.getMessage('users_portal_password')}>
                            <button
                                type="button"
                                class={theme.table.action}
                                disabled={usersState.processingSave}
                                onClick={() => setPortalUser(u)}
                                data-testid="user-portal-password"
                            >
                                <Icon icon="user" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('users_portal_password')}
                                </span>
                            </button>
                        </Tooltip>

                        <Tooltip content={intl.getMessage('users_delete')}>
                            <button
                                type="button"
                                class={cn(theme.table.action, theme.table.action_danger)}
                                disabled={usersState.processingSave}
                                onClick={() => {
                                    setSelected([u.uid || '']);
                                    setConfirmKind('delete');
                                }}
                            >
                                <Icon icon="delete" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('users_delete')}
                                </span>
                            </button>
                        </Tooltip>
                    </div>
                ),
            },
        ],
    );

    return (
        <div class={theme.layout.container}>
            <div class={theme.layout.containerIn}>
                <h1
                    class={cn(theme.layout.title, theme.title.h4, theme.title.h3_tablet)}
                    data-testid="users-title"
                >
                    {intl.getMessage('users_title')}
                </h1>

                <div class={s.body}>
                    <p class={s.desc}>{intl.getMessage('users_desc')}</p>

                    <div class={s.stats}>
                        <div class={s.statCard}>
                            <span class={s.statValue}>
                                {formatNumber(usersState.summary.total)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_total')}
                            </span>
                        </div>
                        <div class={s.statCard}>
                            <span class={cn(s.statValue, s.statOk)}>
                                {formatNumber(usersState.summary.active)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_active')}
                            </span>
                        </div>
                        <div class={s.statCard}>
                            <span class={cn(s.statValue, s.statWarn)}>
                                {formatNumber(usersState.summary.expiring_soon)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_expiring')}
                            </span>
                        </div>
                        <div class={s.statCard}>
                            <span class={cn(s.statValue, s.statErr)}>
                                {formatNumber(usersState.summary.over_quota)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_over_quota')}
                            </span>
                        </div>
                        <div class={s.statCard}>
                            <span class={cn(s.statValue, s.statErr)}>
                                {formatNumber(usersState.summary.expired)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_expired')}
                            </span>
                        </div>
                        <div class={s.statCard}>
                            <span class={s.statValue}>
                                {formatNumber(usersState.summary.disabled)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_disabled')}
                            </span>
                        </div>
                        <div class={cn(s.statCard, s.statCardWide)}>
                            <span class={s.statValue}>
                                {formatNumber(usersState.summary.requests)}
                            </span>
                            <span class={s.statLabel}>
                                {intl.getMessage('users_stat_requests')}
                            </span>
                        </div>
                    </div>

                    <div class={s.toolbar}>
                        <Input
                            class={s.search}
                            size="small"
                            value={search()}
                            placeholder={intl.getMessage('users_search_placeholder')}
                            prefixIcon={<Icon icon="search" />}
                            isClearable
                            onClear={() => setSearch('')}
                            onChange={(e) => setSearch(e.currentTarget.value)}
                            data-testid="users-search"
                        />

                        <div class={s.chips}>
                            <button
                                type="button"
                                class={cn(s.chip, {
                                    [s.chipActive]: statusFilter() === 'all',
                                })}
                                onClick={() => setStatusFilter('all')}
                            >
                                {intl.getMessage('users_filter_all')}
                                <span class={s.chipCount}>{usersState.summary.total ?? 0}</span>
                            </button>
                            <For each={STATUSES}>
                                {(status) => (
                                    <Show when={statusCount(status) > 0}>
                                        <button
                                            type="button"
                                            class={cn(s.chip, {
                                                [s.chipActive]: statusFilter() === status,
                                            })}
                                            onClick={() => setStatusFilter(status)}
                                        >
                                            {statusText(status)}
                                            <span class={s.chipCount}>
                                                {statusCount(status)}
                                            </span>
                                        </button>
                                    </Show>
                                )}
                            </For>
                        </div>

                        <div class={s.toolbarActions}>
                            <button
                                type="button"
                                class={s.importButton}
                                onClick={() => setBackupsShown(true)}
                                data-testid="users-backups-button"
                            >
                                {intl.getMessage('users_backups')}
                            </button>

                            <button
                                type="button"
                                class={s.importButton}
                                onClick={() => setBulkAddShown(true)}
                                data-testid="users-bulk-add-button"
                            >
                                {intl.getMessage('users_bulk_add')}
                            </button>

                            <PlusButton
                                onClick={() => setEdited(null)}
                                testId="users-add-button"
                            >
                                {intl.getMessage('users_add')}
                            </PlusButton>
                        </div>
                    </div>

                    <Show when={selected().length > 0}>
                        <div class={s.bulkBar}>
                            <span class={s.bulkText}>
                                {intl.getMessage('users_selected', {
                                    count: selected().length,
                                })}
                            </span>
                            <div class={s.bulkActions}>
                                <button
                                    type="button"
                                    class={s.bulkButton}
                                    disabled={usersState.processingSave}
                                    onClick={() => toggleUsers(selected(), true)}
                                >
                                    {intl.getMessage('users_enable')}
                                </button>
                                <button
                                    type="button"
                                    class={s.bulkButton}
                                    disabled={usersState.processingSave}
                                    onClick={() => toggleUsers(selected(), false)}
                                >
                                    {intl.getMessage('users_disable')}
                                </button>
                                <button
                                    type="button"
                                    class={s.bulkButton}
                                    disabled={usersState.processingSave}
                                    onClick={() => setConfirmKind('reset')}
                                >
                                    {intl.getMessage('users_reset')}
                                </button>
                                <button
                                    type="button"
                                    class={cn(s.bulkButton, s.bulkButtonDanger)}
                                    disabled={usersState.processingSave}
                                    onClick={() => setConfirmKind('delete')}
                                >
                                    {intl.getMessage('users_delete')}
                                </button>
                            </div>
                        </div>
                    </Show>

                    <div class={s.tableSection}>
                        <Table
                            data={filtered()}
                            columns={columns()}
                            loading={!usersState.initialized}
                            pageSize={20}
                            getRowId={(u) => u.uid || ''}
                            class={s.tableRows}
                            tableRowClass={s.cardRow}
                            emptyTable={
                                <div class={s.empty}>
                                    <Icon icon="user" class={s.emptyIcon} />
                                    <span>{intl.getMessage('users_empty')}</span>
                                </div>
                            }
                        />
                    </div>
                </div>
            </div>

            <Show when={edited() !== undefined}>
                <UserDialog user={edited() || null} onClose={() => setEdited(undefined)} />
            </Show>

            <Show when={bulkAddShown()}>
                <BulkAddDialog onClose={() => setBulkAddShown(false)} />
            </Show>

            <Show when={portalUser()}>
                <PortalPasswordDialog
                    user={portalUser() as User}
                    onClose={() => setPortalUser(null)}
                />
            </Show>

            <Show when={backupsShown()}>
                <BackupsDialog onClose={() => setBackupsShown(false)} />
            </Show>

            <Show when={confirmKind()}>
                <ConfirmDialog
                    onClose={() => setConfirmKind('')}
                    onConfirm={handleConfirm}
                    submitDisabled={usersState.processingSave}
                    buttonText={
                        confirmKind() === 'delete'
                            ? intl.getMessage('yes_remove')
                            : intl.getMessage('users_reset')
                    }
                    cancelText={intl.getMessage('cancel')}
                    buttonVariant={confirmKind() === 'delete' ? 'danger' : 'primary'}
                    title={
                        confirmKind() === 'delete'
                            ? intl.getMessage('users_delete_title')
                            : intl.getMessage('users_reset_title')
                    }
                    text={
                        confirmKind() === 'delete'
                            ? intl.getMessage('users_delete_desc', {
                                  count: selected().length,
                              })
                            : intl.getMessage('users_reset_desc', {
                                  count: selected().length,
                              })
                    }
                />
            </Show>
        </div>
    );
};
