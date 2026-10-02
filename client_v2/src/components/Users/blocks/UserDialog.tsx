import { createSignal, Show, untrack } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Dialog } from 'panel/common/ui/Dialog';
import { Input } from 'panel/common/controls/Input';
import { Switch } from 'panel/common/controls/Switch';
import type { User } from 'panel/api/model';
import type { UserAddRequest, UserUpdateRequest } from 'panel/api/model';
import { addUser, updateUser } from 'panel/stores/users';

import s from './UserDialog.module.pcss';

type Props = {
    /** user is the user to edit, or null to create a new one. */
    user: User | null;

    onClose: () => void;
};

/** defaultExpireDays is the expiry a new user gets. */
const defaultExpireDays = 30;

/** parseIDs splits a textarea value into a list of identifiers. */
const parseIDs = (value: string): string[] =>
    value
        .split(/[\n,;]+/)
        .map((id) => id.trim())
        .filter(Boolean);

/**
 * remainingDays returns the whole number of days left before the user expires,
 * or zero when the user never expires.
 */
const remainingDays = (u: User | null): number => {
    const seconds = u?.remaining_seconds ?? -1;
    if (seconds < 0) {
        return 0;
    }

    return Math.max(0, Math.ceil(seconds / 86400));
};

/**
 * randomID returns a short identifier that is valid as a hostname label.  The
 * alphabet leaves out the vowels and the look-alike characters, so the
 * identifier stays easy to read out loud and impossible to confuse with
 * another one.
 */
const randomID = (): string => {
    const alphabet = 'bcdfghjkmnpqrstvwxyz23456789';
    const bytes = new Uint8Array(8);
    crypto.getRandomValues(bytes);

    return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
};

export const UserDialog = (props: Props) => {
    const isNew = () => props.user === null;

    const [name, setName] = createSignal(untrack(() => props.user?.name) || '');
    const [remark, setRemark] = createSignal(untrack(() => props.user?.remark) || '');
    const [ids, setIDs] = createSignal((untrack(() => props.user?.ids) || []).join('\n'));
    const [unlimited, setUnlimited] = createSignal(
        (untrack(() => props.user?.request_limit) ?? -1) < 0,
    );
    const [limit, setLimit] = createSignal(
        (untrack(() => props.user?.request_limit) ?? -1) < 0
            ? 10000
            : (untrack(() => props.user?.request_limit) ?? 10000),
    );
    const [period, setPeriod] = createSignal<string>(untrack(() => props.user?.period) || 'day');

    // The expiry is always an absolute number of days counted from today, never
    // a delta on the stored date: a delta would silently push the date further
    // out on every save.  The field is seeded with the days the user has left
    // and is only sent when the administrator actually changes it, so saving an
    // untouched form leaves the expiration date exactly where it was.
    const expireInitial = untrack(() =>
        isNew() ? defaultExpireDays : remainingDays(props.user),
    );
    const [expireDays, setExpireDays] = createSignal(expireInitial);

    const [enabled, setEnabled] = createSignal(untrack(() => props.user?.enabled) ?? true);
    const [error, setError] = createSignal('');

    /** replaceRandomID replaces the identifiers with one generated value. */
    const replaceRandomID = () => {
        setIDs(randomID());
    };

    const submit = async () => {
        const parsedIDs = parseIDs(ids());

        if (!name().trim()) {
            setError(intl.getMessage('users_err_name'));

            return;
        }

        if (parsedIDs.length === 0) {
            setError(intl.getMessage('users_err_ids'));

            return;
        }

        const requestLimit = unlimited() ? -1 : Math.max(0, limit());

        let ok: boolean;

        if (isNew()) {
            const payload: UserAddRequest = {
                name: name().trim(),
                remark: remark().trim(),
                ids: parsedIDs,
                request_limit: requestLimit,
                period: period() as UserAddRequest['period'],
                expire_days: Math.max(0, expireDays()),
                enabled: enabled(),
            };

            ok = await addUser(payload);
        } else {
            const payload: UserUpdateRequest = {
                uid: props.user?.uid || '',
                name: name().trim(),
                remark: remark().trim(),
                ids: parsedIDs,
                request_limit: requestLimit,
                period: period() as UserUpdateRequest['period'],
                enabled: enabled(),
            };

            if (expireDays() !== expireInitial) {
                payload.expire_days = Math.max(0, expireDays());
            }

            ok = await updateUser(payload);
        }

        if (ok) {
            props.onClose();
        }
    };

    return (
        <Dialog
            visible
            onClose={props.onClose}
            title={intl.getMessage(isNew() ? 'users_add_title' : 'users_edit_title')}
            class={s.dialog}
        >
            <div class={s.form}>
                {/* The name and the note describe the same client, so they
                    share a row instead of taking one each. */}
                <div class={s.row}>
                    <div class={s.textField}>
                        <label class={s.fieldLabel} for="user-name">
                            {intl.getMessage('users_field_name')}
                        </label>
                        <Input
                            id="user-name"
                            size="small"
                            class={s.inputSmall}
                            value={name()}
                            onChange={(e) => setName(e.currentTarget.value)}
                            data-testid="user-name"
                        />
                    </div>

                    <div class={s.textField}>
                        <label class={s.fieldLabel} for="user-remark">
                            {intl.getMessage('users_field_remark')}
                        </label>
                        <Input
                            id="user-remark"
                            size="small"
                            class={s.inputSmall}
                            value={remark()}
                            onChange={(e) => setRemark(e.currentTarget.value)}
                            data-testid="user-remark"
                        />
                    </div>
                </div>

                <div class={s.textField}>
                    <div class={s.fieldHead}>
                        <label class={s.fieldLabel} for="user-ids">
                            {intl.getMessage('users_field_ids')}
                        </label>
                        <button
                            type="button"
                            class={s.randomButton}
                            onClick={replaceRandomID}
                            data-testid="user-random-id"
                        >
                            {intl.getMessage('users_random_id')}
                        </button>
                    </div>
                    <textarea
                        id="user-ids"
                        class={s.textarea}
                        rows={1}
                        placeholder={intl.getMessage('users_field_ids_placeholder')}
                        value={ids()}
                        onChange={(e) => setIDs(e.currentTarget.value)}
                        data-testid="user-ids"
                    />
                    <div class={s.hint}>{intl.getMessage('users_field_ids_hint')}</div>
                </div>

                <div class={s.row}>
                    <div class={cn(s.textField, s.fieldNumber)}>
                        <label class={s.fieldLabel} for="user-limit">
                            {intl.getMessage('users_field_limit')}
                        </label>
                        <Input
                            id="user-limit"
                            size="small"
                            class={s.inputSmall}
                            type="number"
                            value={limit()}
                            disabled={unlimited()}
                            onChange={(e) => setLimit(Number(e.currentTarget.value))}
                            data-testid="user-limit"
                        />
                    </div>

                    <div class={s.textField}>
                        <label class={s.fieldLabel} for="user-period">
                            {intl.getMessage('users_field_period')}
                        </label>
                        <select
                            id="user-period"
                            class={s.select}
                            value={period()}
                            disabled={unlimited()}
                            onChange={(e) => setPeriod(e.currentTarget.value)}
                        >
                            <option value="day">{intl.getMessage('users_period_day')}</option>
                            <option value="month">{intl.getMessage('users_period_month')}</option>
                            <option value="total">{intl.getMessage('users_period_total')}</option>
                        </select>
                    </div>
                </div>

                <Switch
                    id="user-unlimited"
                    checked={unlimited()}
                    onChange={() => setUnlimited((prev) => !prev)}
                >
                    {intl.getMessage('users_field_unlimited')}
                </Switch>

                <div class={cn(s.textField, s.fieldNumber)}>
                    <label class={s.fieldLabel} for="user-expire">
                        {intl.getMessage('users_field_expire_days')}
                    </label>
                    <Input
                        id="user-expire"
                        size="small"
                        class={s.inputSmall}
                        type="number"
                        value={expireDays()}
                        onChange={(e) => setExpireDays(Number(e.currentTarget.value))}
                        data-testid="user-expire"
                    />
                    <div class={s.hint}>
                        {intl.getMessage('users_field_expire_hint')}
                        <Show when={!isNew()}>
                            {' '}
                            {intl.getMessage('users_current_expiry', {
                                date: props.user?.expires_at
                                    ? new Date(props.user.expires_at * 1000).toLocaleDateString()
                                    : intl.getMessage('users_never'),
                            })}
                        </Show>
                    </div>
                </div>

                <Switch
                    id="user-enabled"
                    checked={enabled()}
                    onChange={() => setEnabled((prev) => !prev)}
                >
                    {intl.getMessage('users_field_enabled')}
                </Switch>

                <Show when={error()}>
                    <div class={s.error}>{error()}</div>
                </Show>
            </div>

            {/* The footer is a sibling of the padded form so that the shared
                dialog footer keeps its own 16px inset, which lines the buttons
                up with the title instead of nesting one inset inside another. */}
            <div class={cn(theme.dialog.footer, s.footer)}>
                <Button
                    size="small"
                    variant="secondary"
                    onClick={props.onClose}
                    class={theme.dialog.button}
                >
                    {intl.getMessage('cancel')}
                </Button>
                <Button
                    size="small"
                    onClick={submit}
                    class={theme.dialog.button}
                    data-testid="user-save"
                >
                    {intl.getMessage('save')}
                </Button>
            </div>
        </Dialog>
    );
};
