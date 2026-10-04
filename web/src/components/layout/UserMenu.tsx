import React from 'react';
import { useNavigate } from 'react-router-dom';
import { ChevronDown, Languages, LogOut, UserRound } from 'lucide-react';
import { LOCALES, LOCALE_NATIVE, useLocale, useT } from '@/i18n';
import { DropdownMenu } from '@/components/ui/DropdownMenu';
import { useAuth } from '@/context/AuthContext';


const TRIGGER =
  'flex items-center gap-1.5 px-1.5 py-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 text-xs transition-colors';

/** The first letter of the display name (or username), for the avatar chip. */
export function userInitial(name: string | undefined): string {
  const c = (name ?? '').trim().charAt(0);
  return c ? c.toUpperCase() : '?';
}

export function UserAvatar({ name, className = '' }: { name?: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-flex items-center justify-center rounded-full bg-primary/20 text-primary border border-primary/40 font-semibold ${className}`}
    >
      {userInitial(name)}
    </span>
  );
}

// Header user menu (auth-02 §4): avatar initial + display name, with
// Profile, Language (submenu) and Sign out.
export function UserMenu() {
  const { me, logout } = useAuth();
  const navigate = useNavigate();
  const t = useT();
  const { locale, setLocale } = useLocale();
  if (!me) return null;
  const name = me.display_name || me.username;

  return (
    <DropdownMenu
      align="right"
      label={t('nav.accountMenu')}
      triggerClassName={TRIGGER}
      items={[
        { id: 'profile', label: t('nav.profile'), icon: <UserRound />, onClick: () => navigate('/profile') },
        {
          id: 'language',
          label: t('nav.language'),
          icon: <Languages />,
          submenu: LOCALES.map((l) => ({ id: l, label: LOCALE_NATIVE[l], checked: l === locale, onClick: () => setLocale(l) })),
        },
        { id: 'signout', label: t('nav.signOut'), icon: <LogOut />, onClick: () => void logout(), separatorBefore: true },
      ]}
      trigger={
        <span className="flex items-center gap-1.5" data-testid="user-menu">
          <UserAvatar name={name} className="w-6 h-6 text-[11px]" />
          <span className="hidden sm:inline max-w-[10rem] truncate text-foreground">{name}</span>
          <ChevronDown className="w-3 h-3 opacity-60" />
        </span>
      }
    />
  );
}
