import React, { useState } from 'react';

const CyberpunkLogin: React.FC = () => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    console.log('Logging in with:', { username, password });
    alert(`登录成功！\n用户名: ${username}`);
  };

  return (
    <div className="relative min-h-screen w-full flex items-center justify-center bg-cyber-dark bg-grid-pattern overflow-hidden">
      {/* Decorative Side Circuits */}
      <div className="absolute left-0 top-1/2 -translate-y-1/2 w-48 h-64 opacity-40 pointer-events-none">
        <div className="w-full h-[2px] bg-gradient-to-r from-transparent via-cyber-blue to-cyber-blue shadow-[0_0_10px_#00f0ff] absolute top-1/2" />
        <div className="w-32 h-[2px] bg-cyber-blue absolute top-[30%] left-0" />
        <div className="w-24 h-[2px] bg-cyber-blue absolute top-[70%] left-0" />
        <div className="w-[2px] h-16 bg-cyber-blue absolute top-[calc(50%-32px)] left-16" />
        <div className="w-[2px] h-16 bg-cyber-blue absolute top-[calc(50%+16px)] left-16" />
        <div className="w-4 h-4 rounded-full bg-cyber-blue shadow-[0_0_15px_#00f0ff] absolute top-[calc(50%-8px)] left-14" />
      </div>

      <div className="absolute right-0 top-1/2 -translate-y-1/2 w-48 h-64 opacity-40 pointer-events-none">
        <div className="w-full h-[2px] bg-gradient-to-l from-transparent via-cyber-blue to-cyber-blue shadow-[0_0_10px_#00f0ff] absolute top-1/2" />
        <div className="w-32 h-[2px] bg-cyber-blue absolute top-[30%] right-0" />
        <div className="w-24 h-[2px] bg-cyber-blue absolute top-[70%] right-0" />
        <div className="w-[2px] h-16 bg-cyber-blue absolute top-[calc(50%-32px)] right-16" />
        <div className="w-[2px] h-16 bg-cyber-blue absolute top-[calc(50%+16px)] right-16" />
        <div className="w-4 h-4 rounded-full bg-cyber-blue shadow-[0_0_15px_#00f0ff] absolute top-[calc(50%-8px)] right-14" />
      </div>

      {/* Main Card Container */}
      <div className="relative z-10 w-full max-w-md p-1">
        {/* Outer Pink Border */}
        <div className="absolute inset-0 border-2 border-cyber-pink shadow-[0_0_20px_rgba(255,0,255,0.4)] clip-notch" />
        
        {/* Inner Blue Border */}
        <div className="absolute inset-[6px] border border-cyber-blue shadow-[0_0_15px_rgba(0,240,255,0.3)] clip-notch" />

        {/* Content Area */}
        <div className="relative bg-cyber-panel/80 backdrop-blur-sm border border-cyber-blue/30 m-4 p-8 rounded-sm clip-notch flex flex-col gap-6">
          
          {/* Top Accent Line */}
          <div className="absolute top-0 left-1/2 -translate-x-1/2 w-1/3 h-[2px] bg-cyber-blue shadow-[0_0_10px_#00f0ff]" />

          {/* Title */}
          <h1 className="text-4xl font-bold text-center tracking-widest text-white glow-text mt-2">
            用户名
          </h1>

          <form onSubmit={handleSubmit} className="flex flex-col gap-6">
            {/* Username Field */}
            <div className="flex flex-col gap-2">
              <label htmlFor="username" className="text-xl font-medium text-white ml-1">
                请输入用户名
              </label>
              <input
                id="username"
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="请输入用户名"
                className="w-full bg-transparent border border-cyber-blue text-white px-4 py-3 text-lg focus:outline-none focus:shadow-neon-blue transition-all duration-300 placeholder-gray-500"
                required
              />
            </div>

            {/* Password Field */}
            <div className="flex flex-col gap-2">
              <label htmlFor="password" className="text-xl font-medium text-white ml-1">
                密码
              </label>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="请输入密码"
                className="w-full bg-transparent border border-cyber-blue text-white px-4 py-3 text-lg focus:outline-none focus:shadow-neon-blue transition-all duration-300 placeholder-gray-500"
                required
              />
            </div>

            {/* Submit Button */}
            <button
              type="submit"
              className="mt-4 w-full py-4 text-2xl font-bold text-white bg-gradient-to-r from-purple-900 via-purple-700 to-purple-900 border-2 border-cyber-pink shadow-neon-purple hover:scale-[1.02] active:scale-[0.98] transition-all duration-200 cursor-pointer uppercase tracking-wider"
            >
              登录
            </button>
          </form>
        </div>
      </div>
    </div>
  );
};

export default CyberpunkLogin;