import React from 'react';
import { Link } from 'react-router-dom';
import { Lock } from 'lucide-react';
import { Capability } from '@/api/types';
import { useAuth } from '@/context/AuthContext';
import { Card } from '@/components/ui/Card';
import { useT } from '@/i18n';


// Route-level guard for pages a capability unlocks (Settings, Users, new
// strategy/agent). Hiding the nav entry isn't enough - a typed URL or an old
// bookmark still lands here. The backend enforces the same rule.
export function RequireCapability({ cap, children }: { cap: Capability; children: React.ReactNode }) {
  const { can } = useAuth();
  const t = useT();
  if (can(cap)) return <>{children}</>;
  return (
    <div className="container-custom max-w-xl mx-auto mt-12">
      <Card className="p-6 text-center space-y-3" data-testid="no-access">
        <Lock className="w-8 h-8 mx-auto text-muted-foreground" />
        <h1 className="text-lg font-semibold text-foreground">{t('auth.noAccessTitle')}</h1>
        <p className="text-sm text-muted-foreground">{t('auth.noAccessBody')}</p>
        <Link to="/" className="inline-block text-sm text-primary hover:underline">
          {t('auth.backToDashboard')}
        </Link>
      </Card>
    </div>
  );
}
