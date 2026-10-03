import { createSignal } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Dialog } from 'panel/common/ui/Dialog';
import { Input } from 'panel/common/controls/Input';
import type { User } from 'panel/api/model';
import { setUserPassword, usersState } from 'panel/stores/users';

import s from './PortalPasswordDialog.module.pcss';

type Props = {
    user: User;
    onClose: () => void;
};

/**
 * PortalPasswordDialog sets the password the user signs in to the user portal
 * with.  The password is the only way in, so it is never shown back; the dialog
 * only says whether one is set.
 */
export const PortalPasswordDialog = (props: Props) => {
    const [password, setPassword] = createSignal('');

    const submit = async () => {
        const ok = await setUserPassword(props.user.uid || '', password());

        if (ok) {
            props.onClose();
        }
    };

    return (
        <Dialog
            visible
            onClose={props.onClose}
            title={intl.getMessage('users_portal_password')}
            class={s.dialog}
        >
            <div class={s.body}>
                <div class={s.hint}>{intl.getMessage('users_portal_password_hint')}</div>

                <div class={s.state}>
                    {props.user.has_portal_password
                        ? intl.getMessage('users_portal_password_has_access')
                        : intl.getMessage('users_portal_password_no_access')}
                </div>

                <Input
                    id="portal-password"
                    size="small"
                    type="password"
                    class={s.input}
                    value={password()}
                    placeholder={intl.getMessage('users_portal_password_placeholder')}
                    onChange={(e) => setPassword(e.currentTarget.value)}
                    data-testid="portal-password"
                />
            </div>

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
                    disabled={usersState.processingSave}
                    onClick={submit}
                    class={theme.dialog.button}
                    data-testid="portal-password-save"
                >
                    {intl.getMessage('save')}
                </Button>
            </div>
        </Dialog>
    );
};
